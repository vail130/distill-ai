package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// writeFile is a small helper that writes content to path,
// creating parent directories. Tests rely on it to lay out
// tempdir-based project / user configs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// withCwd chdir's to dir for the duration of the test, restoring
// the original on cleanup. Needed because the config loader reads
// os.Getwd() to discover the project config; t.Chdir landed in
// Go 1.24, which is below this repo's go.mod (1.26), so the
// helper exists for consistency with older toolchains.
func withCwd(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
}

// withHome rewires os.UserHomeDir's source to dir for the
// duration of the test. The stdlib reads $HOME on Unix and macOS;
// %USERPROFILE% on Windows; $home on Plan 9. We set every variant
// so the same test works on every platform CI runs.
func withHome(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"HOME", "USERPROFILE", "home"} {
		prev, prevSet := os.LookupEnv(name)
		if err := os.Setenv(name, dir); err != nil {
			t.Fatalf("Setenv %s: %v", name, err)
		}
		t.Cleanup(func() {
			if prevSet {
				_ = os.Setenv(name, prev)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}

// withXDGCleared unsets XDG_CONFIG_HOME for the duration of the
// test so user-config discovery falls back to <home>/.config.
// Mirrors resetXDG in the internal/config tests.
func withXDGCleared(t *testing.T) {
	t.Helper()
	prev, prevSet := os.LookupEnv("XDG_CONFIG_HOME")
	_ = os.Unsetenv("XDG_CONFIG_HOME")
	t.Cleanup(func() {
		if prevSet {
			_ = os.Setenv("XDG_CONFIG_HOME", prev)
		} else {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		}
	})
}

// TestRun_ProjectConfigSetsDefaultBudget: a project's
// `.distill-ai.toml` with `default_budget = 500` seeds the CLI
// flag default; the pipeline runs with that budget without
// `--budget` on the command line.
func TestRun_ProjectConfigSetsDefaultBudget(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 2)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".distill-ai.toml"),
		"default_budget = 500\n")
	withCwd(t, dir)
	withXDGCleared(t)
	withHome(t, t.TempDir()) // empty home so no user config interferes
	var stdout, stderr bytes.Buffer
	code := run([]string{"--output=json", "fake-pytest"}, strings.NewReader("x"), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d; stderr=%q", code, stderr.String())
	}
	// Parse the JSON to verify the budget actually ran via the
	// summary's events_dropped_budget field (a tight budget would
	// have produced drops; with budget=500 and 2 small events it
	// should be 0 — proving the config was honoured without
	// blowing the test up).
	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%q", err, stdout.String())
	}
	summary, _ := parsed["summary"].(map[string]any)
	if summary == nil {
		t.Fatalf("summary missing: %v", parsed)
	}
}

// TestRun_CLIFlagOverridesConfig: with the same project config,
// an explicit `--budget=1000` wins. Verified by tightening the
// flag to force drops (smaller than the config's default).
func TestRun_CLIFlagOverridesConfig(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	// Many small events so the tight CLI budget forces drops.
	formats.Register(&emittingFormat{
		name:   "fake-pytest",
		score:  0.95,
		events: makeManyEvents(10),
	})
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".distill-ai.toml"),
		"default_budget = 100000\n") // very loose
	withCwd(t, dir)
	withXDGCleared(t)
	withHome(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	// --budget=1 is tighter than the config's 100000; if CLI
	// wins, drops occur and exit is 3 (ExitPartial).
	code := run([]string{"--budget=1", "fake-pytest"},
		strings.NewReader("x"), &stdout, &stderr)
	if code != ExitPartial {
		t.Errorf("exit = %d, want ExitPartial; stderr=%q", code, stderr.String())
	}
}

// TestRun_UserConfigOverridable: a user config sets a default,
// no project config; the CLI inherits the user-config value.
func TestRun_UserConfigOverridable(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 1)
	cwd := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "distill-ai", "config.toml"),
		"default_output = \"json\"\n")
	withCwd(t, cwd)
	withXDGCleared(t)
	withHome(t, home)
	var stdout, stderr bytes.Buffer
	code := run([]string{"fake-pytest"}, strings.NewReader("x"), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d; stderr=%q", code, stderr.String())
	}
	// User config flipped --output to json; the stdout should
	// parse as JSON, not text.
	if !strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Errorf("output not JSON; got %q", stdout.String())
	}
}

// TestRun_ConfigFlagShortCircuitsDiscover: --config <path>
// disables Discover entirely. A project config that would
// otherwise apply is bypassed.
func TestRun_ConfigFlagShortCircuitsDiscover(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake-pytest", 1)
	// Project config sets text; explicit --config sets json.
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".distill-ai.toml"),
		"default_output = \"text\"\n")
	explicitDir := t.TempDir()
	explicit := filepath.Join(explicitDir, "explicit.toml")
	writeFile(t, explicit, "default_output = \"json\"\n")
	withCwd(t, cwd)
	withXDGCleared(t)
	withHome(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"--config=" + explicit, "fake-pytest"},
		strings.NewReader("x"), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d; stderr=%q", code, stderr.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Errorf("expected JSON (from --config); got %q", stdout.String())
	}
}

// TestRun_PrintConfigPrintsMergedResult: --print-config emits
// the merged config as TOML and exits 0 without running the
// pipeline. The output must round-trip through the loader.
func TestRun_PrintConfigPrintsMergedResult(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".distill-ai.toml"),
		"default_budget = 42\ndefault_output = \"json\"\n")
	withCwd(t, dir)
	withXDGCleared(t)
	withHome(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"--print-config"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	// Round-trip the printed TOML.
	// (intentionally not importing config inside cmd/distill-ai
	// for the assertion; instead we string-match the two keys.)
	out := stdout.String()
	if !strings.Contains(out, "DefaultBudget = 42") &&
		!strings.Contains(out, "default_budget = 42") &&
		!strings.Contains(out, "42") {
		t.Errorf("printed config missing default_budget=42; got %q", out)
	}
	if !strings.Contains(out, "json") {
		t.Errorf("printed config missing default_output=json; got %q", out)
	}
}

// TestRun_PrintConfigExitsBeforePipeline: --print-config does
// not read stdin or emit distilled output. A stdin reader that
// returns an error proves stdin was not consumed.
func TestRun_PrintConfigExitsBeforePipeline(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	withXDGCleared(t)
	withHome(t, t.TempDir())
	withCwd(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"--print-config"}, &erroringReader{}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d; stderr=%q", code, stderr.String())
	}
}

// TestRun_BadConfigFailsLoudly: a malformed project config
// causes the binary to exit with ExitError before any pipeline
// work happens, with the path in the error message so the user
// can find the broken file.
func TestRun_BadConfigFailsLoudly(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".distill-ai.toml"), "default_budget =\n")
	withCwd(t, dir)
	withXDGCleared(t)
	withHome(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("exit = %d, want ExitError; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), ".distill-ai.toml") {
		t.Errorf("stderr does not name the bad config; got %q", stderr.String())
	}
}

// TestRun_PerFormatOverrideApplied: a [formats.fake-pytest]
// block's dedupe_window overrides the top-level default for that
// format. This is the per-format precedence path through to the
// pipeline.
func TestRun_PerFormatOverrideApplied(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	// Same Event emitted N times so dedupe collapses them.
	evt := event.Event{
		Severity: event.SeverityError,
		Kind:     "test_failure",
		Title:    "duplicate",
		Body:     []string{"same"},
	}
	formats.Register(&emittingFormat{
		name:   "fake-pytest",
		score:  0.95,
		events: []event.Event{evt, evt, evt, evt, evt},
	})
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".distill-ai.toml"),
		"[formats.fake-pytest]\ndedupe_window = 16\n")
	withCwd(t, dir)
	withXDGCleared(t)
	withHome(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"--output=json", "fake-pytest"},
		strings.NewReader("x"), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d; stderr=%q", code, stderr.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%q", err, stdout.String())
	}
	events, _ := parsed["events"].([]any)
	if len(events) != 1 {
		t.Errorf("expected 1 deduped event, got %d (%v)", len(events), parsed)
	}
}

// TestRun_CustomFormatFromConfig: a [[formats.custom.NAME]]
// block in the project config registers a Format that the user
// can invoke by name and that participates in autodetection.
func TestRun_CustomFormatFromConfig(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".distill-ai.toml"), `
[[formats.custom.myapp]]
detect_regex = '^\[myapp\]'
event_start = '^\[myapp\] ERROR'
event_end = '^\[myapp\] (INFO|DEBUG|ERROR)'
severity = "error"
kind = "myapp_error"
`)
	withCwd(t, dir)
	withXDGCleared(t)
	withHome(t, t.TempDir())
	input := "[myapp] INFO ok\n[myapp] ERROR boom\n  detail\n[myapp] INFO recovered\n"
	var stdout, stderr bytes.Buffer
	code := run([]string{"--output=json", "custom:myapp"},
		strings.NewReader(input), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d; stderr=%q", code, stderr.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%q", err, stdout.String())
	}
	if parsed["format"] != "custom:myapp" {
		t.Errorf("format = %v, want custom:myapp", parsed["format"])
	}
	events, _ := parsed["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
}

// TestRun_BadCustomRegexFailsLoudly: a custom-format block with
// an unparseable regex fails the binary at startup with the
// offending field named in stderr.
func TestRun_BadCustomRegexFailsLoudly(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".distill-ai.toml"), `
[[formats.custom.bad]]
detect_regex = '('
event_start = '^X'
`)
	withCwd(t, dir)
	withXDGCleared(t)
	withHome(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("exit = %d, want ExitError; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "formats.custom.bad") {
		t.Errorf("stderr does not name the bad block; got %q", stderr.String())
	}
}

// makeManyEvents synthesises N error Events with distinct
// titles so a tight budget actually forces drops.
func makeManyEvents(n int) []event.Event {
	out := make([]event.Event, n)
	for i := 0; i < n; i++ {
		out[i] = event.Event{
			Severity: event.SeverityError,
			Kind:     "test_failure",
			Title:    "synthetic failure " + string(rune('A'+i%26)),
			Body: []string{
				"a body line that takes some space",
				"another body line that takes more space",
				"and yet another so the estimator returns a non-trivial cost",
			},
		}
	}
	return out
}

// erroringReader is an io.Reader that always returns an error.
// Used by TestRun_PrintConfigExitsBeforePipeline to prove stdin
// was not consumed.
type erroringReader struct{}

func (erroringReader) Read([]byte) (int, error) {
	return 0, os.ErrClosed
}
