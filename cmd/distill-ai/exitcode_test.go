package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// The tests in this file are the M8.3 DoD set: they exercise the
// four exit codes from named constants rather than literal numbers.
// Several of them overlap with run_test.go cases — that's by design,
// per the alignment rule: M8.3 is the milestone that promotes
// the codes from magic numbers to constants, and the tests are
// anchored to the constants.

// TestExitCode_OK — a successful run with events emerging exits 0.
func TestExitCode_OK(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 2)
	var stdout, stderr bytes.Buffer
	code := run([]string{"fake"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitOK {
		t.Errorf("exit code = %d, want ExitOK (%d); stderr=%q", code, ExitOK, stderr.String())
	}
}

// TestExitCode_NoEvents — clean input → ExitNoEvents.
func TestExitCode_NoEvents(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 0)
	var stdout, stderr bytes.Buffer
	code := run([]string{"fake"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitNoEvents {
		t.Errorf("exit code = %d, want ExitNoEvents (%d); stderr=%q",
			code, ExitNoEvents, stderr.String())
	}
}

// TestExitCode_FlagParseError — an unknown flag returns ExitError.
func TestExitCode_FlagParseError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--definitely-not-a-flag"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("exit code = %d, want ExitError (%d); stderr=%q",
			code, ExitError, stderr.String())
	}
}

// TestExitCode_PipelineError — a parser that errors during Parse
// causes the pipeline to fail with ExitError. The fixture uses an
// errParseFormat that returns an error from Parse instead of an
// events channel.
func TestExitCode_PipelineError(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&errParseFormat{name: "errfmt", score: 0.95, err: errors.New("synthetic parse failure")})
	var stdout, stderr bytes.Buffer
	code := run([]string{"errfmt"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitError {
		t.Errorf("exit code = %d, want ExitError (%d); stderr=%q stdout=%q",
			code, ExitError, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "synthetic parse failure") {
		t.Errorf("stderr should propagate parse error message; got %q", stderr.String())
	}
}

// TestExitCode_BudgetForcedDrops — a tight --budget drops events
// → ExitPartial wins over ExitNoEvents.
func TestExitCode_BudgetForcedDrops(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 50)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--budget=5", "fake"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitPartial {
		t.Errorf("exit code = %d, want ExitPartial (%d); stderr=%q",
			code, ExitPartial, stderr.String())
	}
}

// TestExitCode_PartialBeatsNoEvents — when the budget drops every
// event so nothing emerges, the exit code is ExitPartial not
// ExitNoEvents. The CLI's precedence rule.
func TestExitCode_PartialBeatsNoEvents(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	makeRunFixtureFormat(t, "fake", 50)
	// A tiny budget that can't accommodate even one event title,
	// let alone the 30-token reserve.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--budget=1", "fake"}, strings.NewReader("input"), &stdout, &stderr)
	if code != ExitPartial {
		t.Errorf("exit code = %d, want ExitPartial (%d); stderr=%q",
			code, ExitPartial, stderr.String())
	}
}

// TestExitCode_Constants — anchors the numeric values so accidental
// renumbering would be caught. The values are part of the public
// CLI contract (agents and CI scripts depend on them).
func TestExitCode_Constants(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"ExitOK", ExitOK, 0},
		{"ExitNoEvents", ExitNoEvents, 1},
		{"ExitError", ExitError, 2},
		{"ExitPartial", ExitPartial, 3},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d (changing this number breaks agent contracts)",
				tc.name, tc.got, tc.want)
		}
	}
}

// -----------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------

// errParseFormat is the test fixture for TestExitCode_PipelineError.
// Detect succeeds; Parse returns a non-nil error so Pipeline.Run
// fails before any events flow.
type errParseFormat struct {
	name  string
	score event.Confidence
	err   error
}

func (f *errParseFormat) Name() string                     { return f.name }
func (f *errParseFormat) Detect(_ []byte) event.Confidence { return f.score }
func (f *errParseFormat) Parse(_ context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	return nil, f.err
}
