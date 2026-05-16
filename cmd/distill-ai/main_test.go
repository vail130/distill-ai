package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// fakeFormat mirrors the test helpers in other packages: a Format
// with a controllable Name and confidence. Parse is unused for
// detect-subcommand tests.
type fakeFormat struct {
	name  string
	score event.Confidence
}

func (f *fakeFormat) Name() string                     { return f.name }
func (f *fakeFormat) Detect(_ []byte) event.Confidence { return f.score }
func (f *fakeFormat) Parse(_ context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	ch := make(chan event.Event)
	close(ch)
	return ch, nil
}

func TestRun_HelpReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("--help did not print usage; got %q", stdout.String())
	}
}

func TestRun_VersionReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "distill-ai") {
		t.Errorf("--version output missing program name: %q", stdout.String())
	}
}

func TestRun_NoArgsPrintsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_UnknownSubcommandReturnsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Errorf("stderr did not mention unknown subcommand: %q", stderr.String())
	}
}

// TestDetectCmd_PrintsExpectedFormat registers a fake format with
// high confidence, runs the detect subcommand against a temp file,
// and parses the stable key: value lines.
func TestDetectCmd_PrintsExpectedFormat(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&fakeFormat{name: "pytest", score: 0.95})
	formats.Register(&fakeFormat{name: "jest", score: 0.4})
	path := writeTempFile(t, "some input")
	var stdout, stderr bytes.Buffer
	code := run([]string{"detect", path}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"format: pytest",
		"confidence: 0.95",
		"fellback_to_generic: false",
		"runner: jest",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

func TestDetectCmd_StdinDash(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&fakeFormat{name: "pytest", score: 0.9})
	var stdout, stderr bytes.Buffer
	code := run([]string{"detect", "-"}, strings.NewReader("piped input"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "source: stdin") {
		t.Errorf("stdin source not labelled; got: %q", stdout.String())
	}
}

func TestDetectCmd_HelpfulOutputOnUnknown(t *testing.T) {
	// No formats registered → the detector returns ErrNoFormat.
	// The subcommand must exit 1 with a useful stderr message.
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	path := writeTempFile(t, "completely ambiguous input")
	var stdout, stderr bytes.Buffer
	code := run([]string{"detect", path}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (no match)", code)
	}
	errOut := stderr.String()
	if !strings.Contains(errOut, "no format matched") {
		t.Errorf("stderr did not mention no-match; got: %q", errOut)
	}
}

func TestDetectCmd_FellBackToGenericReturnsOne(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&fakeFormat{name: "pytest", score: 0.3})
	formats.Register(&fakeFormat{name: "generic", score: 0})
	path := writeTempFile(t, "input")
	var stdout, stderr bytes.Buffer
	code := run([]string{"detect", path}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (fell back to generic); stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "fellback_to_generic: true") {
		t.Errorf("output didn't note fallback: %q", stdout.String())
	}
}

func TestDetectCmd_MissingFileArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"detect"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "missing FILE") {
		t.Errorf("stderr missing the diagnostic: %q", stderr.String())
	}
}

func TestDetectCmd_TooManyFileArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"detect", "a", "b"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestDetectCmd_NonexistentFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"detect", "/nonexistent/path/should/not/exist"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

// writeTempFile creates a temp file with content and returns its path.
// t.Cleanup ensures it's removed.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
