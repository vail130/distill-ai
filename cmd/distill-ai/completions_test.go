package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestCompletions_BashOutputIsNonEmpty — bash completion script
// must be non-empty and look like a bash function. We don't pin
// the exact contents because cobra's generator may evolve;
// asserting on a bash-script marker keeps the test honest
// without over-specifying.
func TestCompletions_BashOutputIsNonEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"completions", "bash"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if out == "" {
		t.Fatal("bash completion output is empty")
	}
	// Cobra's bash completion always declares __start_distill-ai
	// somewhere in the script. If a future cobra release changes
	// the convention, update this anchor.
	if !strings.Contains(out, "distill-ai") {
		t.Errorf("bash completion does not mention distill-ai; got %q", out[:min(200, len(out))])
	}
}

// TestCompletions_ZshOutputIsNonEmpty — zsh completion script
// non-empty and references the binary name.
func TestCompletions_ZshOutputIsNonEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"completions", "zsh"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if out == "" {
		t.Fatal("zsh completion output is empty")
	}
	if !strings.Contains(out, "distill-ai") {
		t.Errorf("zsh completion does not mention distill-ai; got %q", out[:min(200, len(out))])
	}
}

// TestCompletions_FishOutputIsNonEmpty — fish completion script
// non-empty.
func TestCompletions_FishOutputIsNonEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"completions", "fish"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	if stdout.String() == "" {
		t.Fatal("fish completion output is empty")
	}
}

// TestCompletions_PowershellOutputIsNonEmpty — powershell
// completion script non-empty.
func TestCompletions_PowershellOutputIsNonEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"completions", "powershell"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	if stdout.String() == "" {
		t.Fatal("powershell completion output is empty")
	}
}

// TestCompletions_UnknownShellErrors — cobra rejects an unknown
// shell argument via ValidArgs and returns ExitError.
func TestCompletions_UnknownShellErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"completions", "tcsh"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("exit code = %d, want ExitError; stderr=%q", code, stderr.String())
	}
}

// TestCompletions_MissingShellErrors — no shell argument at all
// fails with ExitError per cobra.ExactArgs(1).
func TestCompletions_MissingShellErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"completions"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("exit code = %d, want ExitError; stderr=%q", code, stderr.String())
	}
}
