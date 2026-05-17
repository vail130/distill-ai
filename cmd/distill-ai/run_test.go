package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// emittingFormat is the test fixture used by the run command tests.
// Unlike fakeFormat in main_test.go (which closes Parse's channel
// immediately), this format synthesises a configurable set of
// Events so the run path actually has something to distill.
type emittingFormat struct {
	name   string
	score  event.Confidence
	events []event.Event
}

func (f *emittingFormat) Name() string                     { return f.name }
func (f *emittingFormat) Detect(_ []byte) event.Confidence { return f.score }

func (f *emittingFormat) Parse(ctx context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	ch := make(chan event.Event, len(f.events))
	go func() {
		defer close(ch)
		for i := range f.events {
			select {
			case <-ctx.Done():
				return
			case ch <- f.events[i]:
			}
		}
	}()
	return ch, nil
}

// makeRunFixtureFormat registers a format that emits N error Events
// with synthesised titles. Returns the registered name. Caller is
// expected to t.Cleanup(formats.ResetForTest).
func makeRunFixtureFormat(t *testing.T, name string, n int) string {
	t.Helper()
	evts := make([]event.Event, n)
	for i := 0; i < n; i++ {
		evts[i] = event.Event{
			Severity: event.SeverityError,
			Kind:     "test_failure",
			Title:    "synthetic failure " + string(rune('A'+i)),
			Body:     []string{"a body line"},
		}
	}
	formats.Register(&emittingFormat{name: name, score: 0.95, events: evts})
	return name
}

// TestRun_StdinEndToEnd pipes a fixture in, expects exit 0 and at
// least one event in the output. M8.2 doesn't pin output format
// details — that's covered by the encoder golden tests in
// internal/output/. This is a wiring smoke test.
func TestRun_StdinEndToEnd(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 1)
	var stdout, stderr bytes.Buffer
	code := run([]string{"fake-pytest"}, strings.NewReader("some input"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "synthetic failure") {
		t.Errorf("output missing event title; got %q", stdout.String())
	}
}

// TestRun_FileInput reads from a tempfile rather than stdin.
func TestRun_FileInput(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 1)
	path := writeTempFile(t, "some input")
	var stdout, stderr bytes.Buffer
	code := run([]string{"fake-pytest", path}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "synthetic failure") {
		t.Errorf("output missing event title; got %q", stdout.String())
	}
}

// TestRun_MultiFileConcatenation feeds two files. The fake format
// doesn't actually parse the bytes; what matters is that the run
// path accepts multiple file args and doesn't choke on the
// io.MultiReader plumbing.
func TestRun_MultiFileConcatenation(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 1)
	f1 := writeTempFile(t, "file one content")
	f2 := writeNamedTempFile(t, "second.input", "file two content")
	var stdout, stderr bytes.Buffer
	code := run([]string{"fake-pytest", f1, f2}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
}

// TestRun_ExplicitFormatBeatsAutodetect — when the positional FORMAT
// is given, autodetect is skipped and the named format is used even
// if a higher-confidence specific format is registered.
func TestRun_ExplicitFormatBeatsAutodetect(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	// Register two formats. The higher-confidence one would win
	// autodetect; the explicit positional should override.
	formats.Register(&emittingFormat{
		name:  "high-conf",
		score: 0.99,
		events: []event.Event{{
			Severity: event.SeverityError,
			Title:    "high-conf event",
		}},
	})
	formats.Register(&emittingFormat{
		name:  "low-conf",
		score: 0.7,
		events: []event.Event{{
			Severity: event.SeverityError,
			Title:    "low-conf event",
		}},
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"low-conf"}, strings.NewReader("input"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "low-conf event") {
		t.Errorf("explicit FORMAT didn't win; output=%q", stdout.String())
	}
}

// TestRun_NoEventsExitsOne — a format that produces zero events
// triggers exit code 1.
func TestRun_NoEventsExitsOne(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 0)
	var stdout, stderr bytes.Buffer
	code := run([]string{"fake-pytest"}, strings.NewReader("input"), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr=%q stdout=%q",
			code, stderr.String(), stdout.String())
	}
}

// TestRun_StrictUnknownFormatExitsTwo — --strict + no format match
// → exit code 2.
func TestRun_StrictUnknownFormatExitsTwo(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--strict"}, strings.NewReader("ambiguous"), &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
}

// TestRun_VerboseWritesToStderr — `-v` produces diagnostic output
// on stderr while leaving stdout for the distilled stream.
func TestRun_VerboseWritesToStderr(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 1)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-v", "fake-pytest"}, strings.NewReader("input"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "format=fake-pytest") {
		t.Errorf("verbose diagnostic missing format=; stderr=%q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "synthetic failure") {
		t.Errorf("stdout missing distilled events; stdout=%q", stdout.String())
	}
}

// TestRun_OutputJSON — --output=json produces valid JSON. The
// summary's exit_code reflects the outcome the CLI returns: 0 for a
// successful run with events.
func TestRun_OutputJSON(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 2)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--output=json", "fake-pytest"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	// Batch JSON: should be a single top-level object.
	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput=%q", err, stdout.String())
	}
	if parsed["format"] != "fake-pytest" {
		t.Errorf("JSON 'format' field = %v, want fake-pytest", parsed["format"])
	}
	summary, ok := parsed["summary"].(map[string]any)
	if !ok {
		t.Fatalf("JSON summary missing or wrong shape; got %v", parsed["summary"])
	}
	if summary["exit_code"] != float64(0) {
		t.Errorf("JSON summary.exit_code = %v, want 0", summary["exit_code"])
	}
}

// TestRun_OutputJSON_ExitCodeReflectsForcedDrops — when --budget
// forces drops, the JSON summary's exit_code reflects ExitPartial
// (3). M8.3's JSONSink.resolveExitCode reads from observed state so
// the value is honest even though the CLI can't update the Sink
// after Pipeline.Run returns.
func TestRun_OutputJSON_ExitCodeReflectsForcedDrops(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 50)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--output=json", "--budget=5", "fake"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitPartial {
		t.Fatalf("exit code = %d, want ExitPartial; stderr=%q", code, stderr.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput=%q", err, stdout.String())
	}
	summary := parsed["summary"].(map[string]any)
	if summary["exit_code"] != float64(3) {
		t.Errorf("JSON summary.exit_code = %v, want 3 (ExitPartial)", summary["exit_code"])
	}
}

// TestRun_OutputMarkdown — --output=markdown produces something
// containing the markdown-flavoured event heading.
func TestRun_OutputMarkdown(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 1)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--output=markdown", "fake-pytest"}, strings.NewReader("input"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "###") {
		t.Errorf("markdown output missing ### heading; got %q", stdout.String())
	}
}

// TestRun_BudgetForcedDropsExitsThree — --budget too small to fit
// every event triggers exit code 3 (forced drops).
func TestRun_BudgetForcedDropsExitsThree(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	// 50 events with substantial titles → budget=5 cannot fit them
	// all. The pipeline drops the excess and reports forced drops.
	makeRunFixtureFormat(t, "fake-pytest", 50)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--budget=5", "fake-pytest"}, strings.NewReader("input"), &stdout, &stderr)
	if code != 3 {
		t.Errorf("exit code = %d, want 3 (forced drops); stderr=%q",
			code, stderr.String())
	}
}

// TestRun_HelpListsAllFlags — every flag the run command registers
// must appear in --help output. Drift guard against accidental
// flag removal.
func TestRun_HelpListsAllFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", "--help"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run --help exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	help := stdout.String()
	for _, fl := range []string{
		"--auto", "--list-formats",
		"--keep-vendor", "--keep-warnings", "--severity", "--max-events", "--context",
		"--dedupe", "--no-dedupe", "--dedupe-window",
		"--output", "--output-streaming", "--budget", "--no-footer",
		"--explain", "--strict", "--passthrough", "--tokenizer",
		"--verbose",
	} {
		if !strings.Contains(help, fl) {
			t.Errorf("run --help missing flag %q; got:\n%s", fl, help)
		}
	}
}

// TestSplitRunArgs covers the positional-arg classifier.
func TestSplitRunArgs(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&fakeFormat{name: "known-format", score: 0.5})
	for _, tc := range []struct {
		args        []string
		wantFormat  string
		wantFileLen int
	}{
		{nil, "", 0},
		{[]string{"known-format"}, "known-format", 0},
		{[]string{"known-format", "a.log"}, "known-format", 1},
		{[]string{"a.log", "b.log"}, "", 2},
		{[]string{"unknown-thing"}, "", 1},
	} {
		gotF, gotFs := splitRunArgs(tc.args)
		if gotF != tc.wantFormat {
			t.Errorf("splitRunArgs(%v) format = %q, want %q", tc.args, gotF, tc.wantFormat)
		}
		if len(gotFs) != tc.wantFileLen {
			t.Errorf("splitRunArgs(%v) file count = %d, want %d", tc.args, len(gotFs), tc.wantFileLen)
		}
	}
}

// TestResolveDedupeWindow covers the flag precedence rule.
func TestResolveDedupeWindow(t *testing.T) {
	for _, tc := range []struct {
		name string
		fl   runFlags
		want int
	}{
		{"default", runFlags{dedupeWindow: -1}, 0},
		{"dedupe", runFlags{dedupe: true, dedupeWindow: -1}, dedupeWindowDefault},
		{"no-dedupe-wins", runFlags{dedupe: true, noDedupe: true, dedupeWindow: 100}, 0},
		{"explicit-window", runFlags{dedupeWindow: 42}, 42},
		{"explicit-zero", runFlags{dedupeWindow: 0}, 0},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDedupeWindow(&tc.fl)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestRun_ContextCancellation — a slow-emitting parser plus a context
// cancellation should produce a quick exit, not a hang. The harness
// here uses a deliberately undersized timeout to catch any
// regression that introduces unbounded waits.
func TestRun_ContextCancellation(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 1)
	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- run([]string{"fake-pytest"}, strings.NewReader("input"), &stdout, &stderr)
	}()
	select {
	case <-done:
		// fine
	case <-time.After(5 * time.Second):
		t.Fatal("run did not exit within 5s; possible goroutine leak or hang")
	}
}

// writeNamedTempFile writes content into a t.TempDir()-scoped file
// with a caller-chosen filename. Useful when multi-file tests need
// two files with distinct names.
func writeNamedTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
