package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
)

// TestListFormats_OutputShape — the columns are name, version,
// source separated by tabs. With one fixture format registered,
// exactly one line emerges with the expected three columns.
func TestListFormats_OutputShape(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&fakeFormat{name: "fake-one", score: 0.5})
	var stdout, stderr bytes.Buffer
	code := run([]string{"list-formats"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(lines), stdout.String())
	}
	cols := strings.Split(lines[0], "\t")
	if len(cols) != 3 {
		t.Fatalf("got %d columns, want 3: %q", len(cols), lines[0])
	}
	if cols[0] != "fake-one" {
		t.Errorf("name column = %q, want fake-one", cols[0])
	}
	if cols[1] != "1" {
		t.Errorf("version column = %q, want 1", cols[1])
	}
	if cols[2] != "builtin" {
		t.Errorf("source column = %q, want builtin", cols[2])
	}
}

// TestListFormats_DeterministicOrder — running twice produces
// byte-identical output. formats.All() guarantees alphabetical
// order from M1; this anchors that the list-formats output
// preserves it.
func TestListFormats_DeterministicOrder(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	// Register in non-alphabetical order to exercise the sort.
	formats.Register(&fakeFormat{name: "zeta", score: 0.5})
	formats.Register(&fakeFormat{name: "alpha", score: 0.5})
	formats.Register(&fakeFormat{name: "mu", score: 0.5})
	var first, firstErr, second, secondErr bytes.Buffer
	code1 := run([]string{"list-formats"}, strings.NewReader(""), &first, &firstErr)
	code2 := run([]string{"list-formats"}, strings.NewReader(""), &second, &secondErr)
	if code1 != ExitOK || code2 != ExitOK {
		t.Fatalf("exit codes = %d, %d; want both ExitOK", code1, code2)
	}
	if first.String() != second.String() {
		t.Errorf("output differs between calls:\nfirst:\n%s\nsecond:\n%s",
			first.String(), second.String())
	}
	// Cross-check ordering: alpha < mu < zeta.
	wantOrder := "alpha\t1\tbuiltin\nmu\t1\tbuiltin\nzeta\t1\tbuiltin\n"
	if first.String() != wantOrder {
		t.Errorf("got:\n%s\nwant:\n%s", first.String(), wantOrder)
	}
}

// TestListFormats_EmptyRegistry — with no formats registered (the
// production state pre-M9), the subcommand emits nothing and
// exits 0. An empty list is a valid answer to "what's wired".
func TestListFormats_EmptyRegistry(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	var stdout, stderr bytes.Buffer
	code := run([]string{"list-formats"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("expected empty output; got %q", stdout.String())
	}
}

// TestListFormats_FlagFormDelegatesToSubcommand — running
// `distill-ai --list-formats` and `distill-ai list-formats`
// produces the same output. The flag form is a shortcut that
// must stay in sync with the subcommand.
func TestListFormats_FlagFormDelegatesToSubcommand(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&fakeFormat{name: "alpha", score: 0.5})
	formats.Register(&fakeFormat{name: "beta", score: 0.5})
	var flagOut, flagErr, subOut, subErr bytes.Buffer
	flagCode := run([]string{"--list-formats"}, strings.NewReader(""), &flagOut, &flagErr)
	subCode := run([]string{"list-formats"}, strings.NewReader(""), &subOut, &subErr)
	if flagCode != ExitOK || subCode != ExitOK {
		t.Fatalf("exit codes flag=%d sub=%d; want both ExitOK", flagCode, subCode)
	}
	if flagOut.String() != subOut.String() {
		t.Errorf("flag form output diverges from subcommand:\nflag:\n%s\nsubcommand:\n%s",
			flagOut.String(), subOut.String())
	}
}
