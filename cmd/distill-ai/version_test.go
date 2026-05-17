package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestVersionCmd_PrintsBuildInfo — the version subcommand emits
// the three documented fields on separate lines. The ldflag-
// injected values default to "dev" / "none" / "unknown" under
// `go test`; we anchor on the field labels rather than the
// values so the test isn't ldflag-dependent.
func TestVersionCmd_PrintsBuildInfo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"version:", "commit:", "date:"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output missing %q; got:\n%s", want, out)
		}
	}
	// Three lines (plus trailing empty after final \n split).
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %q", len(lines), lines)
	}
}

// TestVersionCmd_NoExtraArgs — passing arguments fails per
// cobra.NoArgs.
func TestVersionCmd_NoExtraArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "extra"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("exit code = %d, want ExitError; stderr=%q", code, stderr.String())
	}
}

// TestVersionCmd_DistinctFromFlag — the subcommand prints the
// fields one per line, whereas the --version flag prints a
// single line. This pins the documented contract that the two
// formats differ.
func TestVersionCmd_DistinctFromFlag(t *testing.T) {
	var subOut, subErr, flagOut, flagErr bytes.Buffer
	subCode := run([]string{"version"}, strings.NewReader(""), &subOut, &subErr)
	flagCode := run([]string{"--version"}, strings.NewReader(""), &flagOut, &flagErr)
	if subCode != ExitOK || flagCode != ExitOK {
		t.Fatalf("exit codes sub=%d flag=%d; want both ExitOK", subCode, flagCode)
	}
	// Subcommand: multi-line. Flag: single line.
	subLines := strings.Count(strings.TrimRight(subOut.String(), "\n"), "\n") + 1
	flagLines := strings.Count(strings.TrimRight(flagOut.String(), "\n"), "\n") + 1
	if subLines == flagLines {
		t.Errorf("subcommand and flag have the same line count (%d); they should differ", subLines)
	}
}
