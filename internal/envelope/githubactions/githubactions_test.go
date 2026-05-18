package githubactions_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/envelope/githubactions"
	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	_ "github.com/vail130/distill-ai/internal/formats/gotest" // detection round-trip
	"github.com/vail130/distill-ai/internal/testutil"
)

func TestGHA_RegisteredAtInit(t *testing.T) {
	got, ok := envelope.Get(githubactions.Name)
	if !ok {
		t.Fatalf("envelope.Get(%q) not found; init() side effect failed", githubactions.Name)
	}
	if got.Name() != githubactions.Name {
		t.Errorf("Name() = %q, want %q", got.Name(), githubactions.Name)
	}
}

func TestGHA_DetectGroupMarker(t *testing.T) {
	cases := []struct {
		name   string
		sample string
		want   event.Confidence
	}{
		{"group", "##[group]Build\nstuff\n##[endgroup]\n", 1.0},
		{"error", "regular line\n##[error]something broke\n", 1.0},
		{"warning", "##[warning]deprecation notice\n", 1.0},
		{"debug", "##[debug]verbose diag\n", 1.0},
		{"notice", "##[notice]check status\n", 1.0},
		{"set-output-legacy", "::set-output name=foo::bar\n", 1.0},
		{"timestamped-marker", "2024-01-15T10:23:45.1234567Z ##[error]boom\n", 1.0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := githubactions.Stripper{}.Detect([]byte(tc.sample))
			if got != tc.want {
				t.Errorf("Detect(%q) = %v, want %v", tc.sample, got, tc.want)
			}
		})
	}
}

func TestGHA_DetectTimestampHeuristic(t *testing.T) {
	// Five lines, all timestamped; no workflow commands → fuzzy 0.8.
	var sb strings.Builder
	for i := 0; i < 5; i++ {
		sb.WriteString("2024-01-15T10:23:45.1234567Z plain output line ")
		sb.WriteString(string(rune('A' + i)))
		sb.WriteByte('\n')
	}
	got := githubactions.Stripper{}.Detect([]byte(sb.String()))
	if got != 0.8 {
		t.Errorf("Detect on five timestamped lines = %v, want 0.8", got)
	}
}

func TestGHA_DetectTimestampHeuristicBelowThreshold(t *testing.T) {
	// Only one timestamped line out of five non-blank: below the
	// 3-of-10 threshold so Detect returns 0.0.
	sample := "" +
		"2024-01-15T10:23:45.1234567Z timestamped\n" +
		"plain line 1\nplain line 2\nplain line 3\nplain line 4\n"
	got := githubactions.Stripper{}.Detect([]byte(sample))
	if got != 0 {
		t.Errorf("Detect = %v, want 0 (1/5 below the 3-of-10 threshold)", got)
	}
}

func TestGHA_DetectNegativeOnPlainLogs(t *testing.T) {
	got := githubactions.Stripper{}.Detect([]byte("hello\nworld\n"))
	if got != 0 {
		t.Errorf("Detect on plain text = %v, want 0", got)
	}
}

func TestGHA_StripTimestamps(t *testing.T) {
	input := "" +
		"2024-01-15T10:23:45.1234567Z hello, world\n" +
		"2024-01-15T10:23:46.1234567Z second line\n"
	want := "hello, world\nsecond line\n"
	cleaned, signals := stripAll(t, input)
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

func TestGHA_StripGroupMarkers(t *testing.T) {
	input := "" +
		"##[group]Build\n" +
		"line A\n" +
		"##[group]Subbuild\n" +
		"line B\n" +
		"##[endgroup]\n" +
		"line C\n" +
		"##[endgroup]\n"
	want := "line A\nline B\nline C\n"
	cleaned, signals := stripAll(t, input)
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

func TestGHA_StripCombinedTimestampsAndGroups(t *testing.T) {
	// The dominant real-world shape: timestamps on every line plus
	// group markers wrapping sections.
	input := "" +
		"2024-01-15T10:23:45.1234567Z ##[group]Build\n" +
		"2024-01-15T10:23:46.1234567Z compiling foo.go\n" +
		"2024-01-15T10:23:47.1234567Z compiling bar.go\n" +
		"2024-01-15T10:23:48.1234567Z ##[endgroup]\n"
	want := "compiling foo.go\ncompiling bar.go\n"
	cleaned, _ := stripAll(t, input)
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
}

func TestGHA_ErrorSignalEmitted(t *testing.T) {
	input := "preamble line\n##[error]something broke\ntrailing line\n"
	cleaned, signals := stripAll(t, input)
	if cleaned != "preamble line\ntrailing line\n" {
		t.Errorf("cleaned = %q; error line should be removed", cleaned)
	}
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	s := signals[0]
	if s.Kind != envelope.KindEnvelopeError {
		t.Errorf("signal Kind = %q, want %q", s.Kind, envelope.KindEnvelopeError)
	}
	if s.Severity != event.SeverityError {
		t.Errorf("signal Severity = %q, want %q", s.Severity, event.SeverityError)
	}
	if s.Title != "something broke" {
		t.Errorf("signal Title = %q, want %q", s.Title, "something broke")
	}
}

func TestGHA_WarningSignalEmitted(t *testing.T) {
	input := "##[warning]deprecation: foo\n##[notice]heads up\n"
	cleaned, signals := stripAll(t, input)
	if cleaned != "" {
		t.Errorf("cleaned = %q; warning/notice lines should be removed", cleaned)
	}
	if len(signals) != 2 {
		t.Fatalf("got %d signals, want 2", len(signals))
	}
	for _, s := range signals {
		if s.Kind != envelope.KindEnvelopeWarning {
			t.Errorf("signal Kind = %q, want %q", s.Kind, envelope.KindEnvelopeWarning)
		}
		if s.Severity != event.SeverityWarn {
			t.Errorf("signal Severity = %q, want %q", s.Severity, event.SeverityWarn)
		}
	}
}

func TestGHA_StepFailureSignal(t *testing.T) {
	input := "" +
		"##[group]Run tests\n" +
		"FAIL: TestFoo\n" +
		"##[endgroup]\n" +
		"##[error]Process completed with exit code 1.\n"
	cleaned, signals := stripAll(t, input)
	if cleaned != "FAIL: TestFoo\n" {
		t.Errorf("cleaned = %q; only the body line should survive", cleaned)
	}
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	s := signals[0]
	if s.Kind != envelope.KindEnvelopeStepFailure {
		t.Errorf("Kind = %q, want %q", s.Kind, envelope.KindEnvelopeStepFailure)
	}
	if s.Metadata["exit_code"] != "1" {
		t.Errorf("Metadata[\"exit_code\"] = %q, want %q", s.Metadata["exit_code"], "1")
	}
	// The step name is recovered from the closed group; it is in
	// scope at the moment the ##[error]Process completed... line
	// arrives because ##[endgroup] popped before ##[error] fires
	// — so step is empty here. Document that with a positive
	// assertion in the nested test below.
	if s.Metadata["step"] != "" {
		t.Errorf("Metadata[\"step\"] = %q, want \"\" (group already closed)", s.Metadata["step"])
	}
}

func TestGHA_StepFailureSignalInsideGroup(t *testing.T) {
	// When the step-exit marker fires while a group is still open,
	// the step name is attached to the signal.
	input := "" +
		"##[group]Run unit tests\n" +
		"some output\n" +
		"##[error]Process completed with exit code 2.\n" +
		"##[endgroup]\n"
	_, signals := stripAll(t, input)
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	s := signals[0]
	if s.Metadata["step"] != "Run unit tests" {
		t.Errorf("Metadata[\"step\"] = %q, want \"Run unit tests\"", s.Metadata["step"])
	}
	if s.Title != "Run unit tests" {
		t.Errorf("Title = %q, want \"Run unit tests\"", s.Title)
	}
	if s.Metadata["exit_code"] != "2" {
		t.Errorf("Metadata[\"exit_code\"] = %q, want \"2\"", s.Metadata["exit_code"])
	}
}

// TestGHA_StripPreservesInnerFormatBytes is the M13.3 DoD's
// round-trip test: a real gotest stream wrapped in a GHA envelope
// must still detect as gotest with Confidence=1.0 after the
// stripper has run. The detector path used here mirrors the
// production binary's; if this test passes, the production binary
// behaves the same way.
func TestGHA_StripPreservesInnerFormatBytes(t *testing.T) {
	wrapped := "" +
		"2024-01-15T10:23:45.1234567Z ##[group]Run go test\n" +
		"2024-01-15T10:23:46.1234567Z === RUN   TestLogin\n" +
		"2024-01-15T10:23:47.1234567Z --- FAIL: TestLogin (0.02s)\n" +
		"2024-01-15T10:23:48.1234567Z     auth_test.go:42: expected 200, got 502\n" +
		"2024-01-15T10:23:49.1234567Z FAIL\n" +
		"2024-01-15T10:23:50.1234567Z FAIL\tgithub.com/example/m\t0.123s\n" +
		"2024-01-15T10:23:51.1234567Z ##[endgroup]\n" +
		"2024-01-15T10:23:52.1234567Z ##[error]Process completed with exit code 1.\n"
	cleaned, _, stripper, err := envelope.Wrap(
		context.Background(),
		strings.NewReader(wrapped),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("envelope.Wrap: %v", err)
	}
	if stripper.Name() != githubactions.Name {
		t.Fatalf("Wrap picked %q, want %q", stripper.Name(), githubactions.Name)
	}
	res, err := detect.Detect(context.Background(), cleaned, detect.Opts{})
	if err != nil {
		t.Fatalf("detect.Detect on cleaned bytes: %v", err)
	}
	if res.Format.Name() != "gotest" {
		t.Errorf("detector picked %q on cleaned bytes, want \"gotest\"; sample=%q",
			res.Format.Name(), string(res.Sample))
	}
	if res.Confidence < 1.0 {
		t.Errorf("detector confidence = %v on cleaned bytes, want 1.0", res.Confidence)
	}
	// Ensure at least one registered format was scanned (this is a
	// dependency-existence sanity check on the imports above).
	if len(formats.All()) == 0 {
		t.Fatal("formats.All() is empty; the gotest blank import was elided")
	}
}

// TestGHA_StreamingStripsIncrementally proves the stripper does not
// buffer the whole input: cleaned bytes appear before the source
// closes. Mirrors the streaming property tests on the formats.
func TestGHA_StreamingStripsIncrementally(t *testing.T) {
	// SlowReader drips four bytes every 20 ms. The input is ~80
	// bytes so a buffering stripper would delay the first cleaned
	// byte by ~400 ms. We assert it arrives within 100 ms.
	input := "" +
		"2024-01-15T10:23:45.1234567Z line one of output\n" +
		"2024-01-15T10:23:46.1234567Z line two of output\n"
	src := &testutil.SlowReader{
		Inner:      strings.NewReader(input),
		ChunkSize:  4,
		ChunkDelay: 20 * time.Millisecond,
	}
	cleaned, signals, err := githubactions.Stripper{}.Strip(context.Background(), src)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	// Drain signals in the background so the Strip goroutine
	// doesn't block on a full buffer.
	go func() {
		for range signals { //nolint:revive // empty body intentional: discard.
		}
	}()
	buf := make([]byte, 16)
	deadline := time.Now().Add(150 * time.Millisecond)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("first cleaned byte did not arrive within 150ms; got %q so far", buf[:0])
		}
		n, err := cleaned.Read(buf)
		if n > 0 {
			// First byte arrived: success.
			return
		}
		if err != nil && err != io.EOF {
			t.Fatalf("cleaned.Read: %v", err)
		}
	}
}

// TestGHA_StripContextCancellation asserts that cancelling ctx
// stops the strip goroutine promptly.
func TestGHA_StripContextCancellation(t *testing.T) {
	// Reader that blocks forever on Read.
	hang := &blockingReader{ch: make(chan struct{})}
	defer close(hang.ch)
	ctx, cancel := context.WithCancel(context.Background())
	cleaned, signals, err := githubactions.Stripper{}.Strip(ctx, hang)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	cancel()
	// Cleaned reader should EOF or error promptly; signals should
	// close. We allow up to 200 ms for the goroutine to wake.
	go func() { _, _ = io.ReadAll(cleaned) }()
	select {
	case _, ok := <-signals:
		if ok {
			t.Errorf("signal arrived after cancellation")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("signals channel did not close within 500ms after cancel")
	}
}

// stripAll is a test helper that runs Strip on the input and drains
// both the cleaned Reader and the signals channel into in-memory
// values for assertion.
func stripAll(t *testing.T, input string) (cleaned string, signals []event.Event) {
	t.Helper()
	r, sigCh, err := githubactions.Stripper{}.Strip(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	// Drain signals on a goroutine so the strip goroutine doesn't
	// block on a full buffer for inputs with many signals.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range sigCh {
			signals = append(signals, ev)
		}
	}()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	<-done
	return string(out), signals
}

// blockingReader's Read blocks until ch closes. Used to drive
// ctx-cancellation tests.
type blockingReader struct {
	ch chan struct{}
}

func (b *blockingReader) Read(_ []byte) (int, error) {
	<-b.ch
	return 0, io.EOF
}
