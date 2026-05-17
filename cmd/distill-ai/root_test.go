package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
)

// The tests in this file are the M8.1 DoD set: they exercise the new
// cobra root command end-to-end through the same run() seam that
// production main() uses. They overlap intentionally with the
// pre-cobra tests in main_test.go — the M8.1 commit keeps the older
// names so reviewers can see the surface is unchanged, and adds
// these so any future cobra refactor has explicit anchors.

// TestRoot_HelpExitsZero — cobra's --help path must exit 0 cleanly
// and write something resembling usage to stdout.
func TestRoot_HelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("--help missing Usage: section; got %q", stdout.String())
	}
	// Cobra renders subcommands in an "Available Commands:" block.
	// The drift test in test/integration/ parses this same shape,
	// so anchor it here too.
	if !strings.Contains(stdout.String(), "detect") {
		t.Errorf("--help did not advertise the detect subcommand; got %q", stdout.String())
	}
}

// TestRoot_UnknownFlagExitsTwo — verifies that cobra-reported
// unknown-flag errors are mapped to exit 2 with the "unknown flag"
// wording cobra emits. Unknown positional verbs no longer route
// through this path (M8.2 made the root accept ArbitraryArgs so
// `cmd | distill-ai pytest` works); see
// TestRun_UnknownPositionalTreatedAsFile for that case.
func TestRoot_UnknownFlagExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--definitely-not-a-real-flag"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Errorf("stderr did not say 'unknown flag'; got %q", stderr.String())
	}
}

// TestRoot_VersionPrintsBuildInfo — cobra has its own --version
// handling; we drove the template through SetVersionTemplate so the
// output matches the pre-cobra shape ("distill-ai <ver> (commit X,
// built Y)").
func TestRoot_VersionPrintsBuildInfo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	// Every variable injected by ldflags should be present somewhere.
	// In `go test` they default to "dev" / "none" / "unknown" so we
	// match those substrings rather than pinning specific values.
	for _, want := range []string{"distill-ai", "commit", "built"} {
		if !strings.Contains(out, want) {
			t.Errorf("--version output missing %q; got %q", want, out)
		}
	}
}

// TestRoot_ShortVerboseFlag — M8.2 reassigned `-v` from --version to
// --verbose, matching the convention in every other Unix CLI. This
// test pins the new meaning so a future regression is visible.
// `--version` is still accepted long-form.
func TestRoot_ShortVerboseFlag(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&fakeFormat{name: "generic", score: 0})
	var stdout, stderr bytes.Buffer
	// `-v` alone is now the verbose flag; with empty stdin the run
	// path reaches "no events" and exits 1. The diagnostic line
	// goes to stderr, proving -v wired to --verbose.
	code := run([]string{"-v"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (empty input under verbose); stderr=%q stdout=%q",
			code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "format=") {
		t.Errorf("-v did not produce verbose diagnostic; stderr=%q", stderr.String())
	}
}

// TestRoot_FactoryProducesFreshCommandEachCall — cobra commands
// carry parsed-flag state. The newRootCmd factory must hand out a
// fresh command per invocation so successive run() calls in tests
// don't carry over flags from earlier calls.
func TestRoot_FactoryProducesFreshCommandEachCall(t *testing.T) {
	var sink bytes.Buffer
	c1 := newRootCmd(strings.NewReader(""), &sink, &sink)
	c2 := newRootCmd(strings.NewReader(""), &sink, &sink)
	if c1 == c2 {
		t.Fatal("newRootCmd returned the same pointer twice; commands must be per-invocation")
	}
}
