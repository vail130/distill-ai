package main

import (
	"bytes"
	"strings"
	"testing"
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

// TestRoot_UnknownSubcommandExitsTwo — verifies that the cobra error
// for unknown verbs is mapped to exit code 2 with the project's
// preferred wording ("subcommand", not cobra's default "command").
func TestRoot_UnknownSubcommandExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"definitely-not-a-real-subcommand"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Errorf("stderr did not say 'unknown subcommand' (cobra's default is 'unknown command'); got %q",
			stderr.String())
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

// TestRoot_ShortVersionFlag — `-v` is a synonym for `--version`
// today. M8.2 will reassign it to --verbose; this test pins the
// current behaviour so the reassignment is a visible, intentional
// change rather than a silent regression.
func TestRoot_ShortVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-v"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "distill-ai") {
		t.Errorf("-v output missing program name: %q", stdout.String())
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
