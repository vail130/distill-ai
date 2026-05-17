package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// TestExplainCmd_AnnotatesKept — basic case: a fixture emitting
// two events produces two "kept" lines.
func TestExplainCmd_AnnotatesKept(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 2)
	var stdout, stderr bytes.Buffer
	code := run([]string{"explain", "fake"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	keptCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "kept") {
			keptCount++
		}
	}
	if keptCount != 2 {
		t.Errorf("got %d kept lines, want 2; output:\n%s", keptCount, stdout.String())
	}
}

// TestExplainCmd_AnnotatesBudgetDrops — a fixture producing many
// events with a tiny --budget triggers BudgetStage drops; the
// explain output reports them on "dropped:budget" lines.
func TestExplainCmd_AnnotatesBudgetDrops(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 20)
	var stdout, stderr bytes.Buffer
	code := run([]string{"explain", "--budget=5", "fake"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitPartial {
		t.Fatalf("exit code = %d, want ExitPartial; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "dropped:budget") {
		t.Errorf("output missing dropped:budget lines; got:\n%s", stdout.String())
	}
}

// TestExplainCmd_DedupeAnnotationInline — when DedupeStage collapses
// duplicates into a Count>1 event, the explain sink renders the
// "<dedupe-evicted=K-1>" annotation on the kept line.
func TestExplainCmd_DedupeAnnotationInline(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	// Register a format that emits four identical events; with
	// --dedupe enabled DedupeStage collapses them into one event
	// with Count=4.
	formats.Register(&emittingFormat{
		name:  "dup",
		score: 0.95,
		events: []event.Event{
			{Severity: event.SeverityError, Title: "same"},
			{Severity: event.SeverityError, Title: "same"},
			{Severity: event.SeverityError, Title: "same"},
			{Severity: event.SeverityError, Title: "same"},
		},
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"explain", "--dedupe", "dup"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "<dedupe-evicted=3>") {
		t.Errorf("missing dedupe-evicted annotation; got:\n%s", stdout.String())
	}
}

// TestExplainCmd_NoFalseDrops — clean input that fits well within
// --budget produces only kept lines, no dropped lines.
func TestExplainCmd_NoFalseDrops(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 1)
	var stdout, stderr bytes.Buffer
	code := run([]string{"explain", "fake"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want ExitOK; stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "dropped:") {
		t.Errorf("clean input should not produce dropped lines; got:\n%s", stdout.String())
	}
}

// TestExplainCmd_HelpListsFlags — `explain --help` includes the
// shared run flags. Drift guard against accidental flag removal
// from registerRunFlags.
func TestExplainCmd_HelpListsFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"explain", "--help"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("explain --help exit = %d, want ExitOK", code)
	}
	for _, fl := range []string{"--budget", "--dedupe", "--strict", "--tokenizer"} {
		if !strings.Contains(stdout.String(), fl) {
			t.Errorf("explain --help missing %q; got:\n%s", fl, stdout.String())
		}
	}
}

// TestExplainCmd_StrictNoFormat — --strict + no matching format
// → ExitError, mirrors run's --strict behaviour.
func TestExplainCmd_StrictNoFormat(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	var stdout, stderr bytes.Buffer
	code := run([]string{"explain", "--strict"}, strings.NewReader("ambiguous"), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("exit code = %d, want ExitError; stderr=%q", code, stderr.String())
	}
}

// TestExplainCmd_FlagFormDelegatesToSubcommand — `--explain` on the
// run command produces the same output as the explain subcommand.
// The flag form is a convenience shortcut; both paths must stay
// in sync.
func TestExplainCmd_FlagFormDelegatesToSubcommand(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 2)
	var flagOut, flagErr, subOut, subErr bytes.Buffer
	flagCode := run([]string{"--explain", "fake"}, strings.NewReader("input"), &flagOut, &flagErr)
	subCode := run([]string{"explain", "fake"}, strings.NewReader("input"), &subOut, &subErr)
	if flagCode != ExitOK || subCode != ExitOK {
		t.Fatalf("exit codes flag=%d sub=%d", flagCode, subCode)
	}
	if flagOut.String() != subOut.String() {
		t.Errorf("flag form output differs from subcommand:\nflag:\n%s\nsubcommand:\n%s",
			flagOut.String(), subOut.String())
	}
}
