package pytest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/pytest"
)

// TestPytest_ParseExtractsFramesLongForm — a fixture that
// exercises the Python long-form traceback frame shape: `File
// "<path>", line N, in <func>`.
func TestPytest_ParseExtractsFramesLongForm(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_db ______________________________

>   raise RuntimeError("nope")
E   RuntimeError: nope

Traceback (most recent call last):
  File "tests/test_db.py", line 15, in test_db
    fetch_user(42)
  File "src/db.py", line 7, in fetch_user
    raise RuntimeError("nope")

src/db.py:7: RuntimeError
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	frames := got[0].Frames
	if len(frames) != 2 {
		t.Fatalf("got %d frames; want 2: %+v", len(frames), frames)
	}
	want := []event.StackFrame{
		{File: "tests/test_db.py", Line: 15, Function: "test_db"},
		{File: "src/db.py", Line: 7, Function: "fetch_user"},
	}
	for i, w := range want {
		if frames[i].File != w.File || frames[i].Line != w.Line || frames[i].Function != w.Function {
			t.Errorf("frame[%d] = %+v, want %+v", i, frames[i], w)
		}
	}
}

// TestPytest_ParseExtractsFramesShortForm — `--tb=short` emits
// frames as `<path>:<line>: in <func>` followed by an indented
// source line. The shortTracebackFramePattern picks them up.
func TestPytest_ParseExtractsFramesShortForm(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_q ______________________________
tests/test_q.py:3: in test_q
    helper()
src/helper.py:9: in helper
    raise RuntimeError
E       RuntimeError
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	frames := got[0].Frames
	if len(frames) != 2 {
		t.Fatalf("got %d frames; want 2: %+v", len(frames), frames)
	}
	if frames[0].Function != "test_q" || frames[1].Function != "helper" {
		t.Errorf("functions = [%q, %q], want [test_q, helper]",
			frames[0].Function, frames[1].Function)
	}
}

// TestPytest_ParseFramesNilOnTbLine — under `--tb=line` pytest
// emits a single-line summary per failure with no frame
// information. Frames must be nil so the M5 CollapseStage
// can tell "parser had no structured data" from "parser
// emitted an empty slice".
func TestPytest_ParseFramesNilOnTbLine(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_x ______________________________
/path/test_x.py:5: AssertionError: nope
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	if got[0].Frames != nil {
		t.Errorf("Frames = %+v, want nil (--tb=line has no frame data)", got[0].Frames)
	}
}

// TestPytest_ParseWarningsEmittedWithKeepWarnings — a fixture
// containing a `=== warnings summary ===` block emits warning
// Events when KeepWarnings is set.
func TestPytest_ParseWarningsEmittedWithKeepWarnings(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_a ______________________________
>       assert False
E       AssertionError
tests/x.py:1: AssertionError
============================== warnings summary ===============================
tests/test_a.py:10
  /path/tests/test_a.py:10: DeprecationWarning: foo is deprecated, use bar
    foo()
=========================== short test summary info ============================
======================== 1 failed, 0 warnings in 0.1s ==========================
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := pytest.Format{}.Parse(ctx, strings.NewReader(input),
		formats.ParseOpts{KeepWarnings: true})
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	got := drain(ch)
	if len(got) != 2 {
		t.Fatalf("got %d events; want 2 (failure + warning): %+v", len(got), got)
	}
	if got[1].Severity != event.SeverityWarn {
		t.Errorf("warning Severity = %q, want warn", got[1].Severity)
	}
	if got[1].Kind != "warning" {
		t.Errorf("warning Kind = %q, want warning", got[1].Kind)
	}
	if !strings.Contains(got[1].Title, "DeprecationWarning") {
		t.Errorf("warning Title = %q; want DeprecationWarning in it", got[1].Title)
	}
}

// TestPytest_ParseWarningsDroppedByDefault — without
// KeepWarnings or an explicit MinSeverity, the warnings summary
// section is parsed but the resulting Events fall below the
// error floor and are filtered out before reaching the channel.
func TestPytest_ParseWarningsDroppedByDefault(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_a ______________________________
>       assert False
E       AssertionError
tests/x.py:1: AssertionError
============================== warnings summary ===============================
tests/test_a.py:10
  /path/tests/test_a.py:10: DeprecationWarning: foo
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1 (failure only, warnings filtered): %+v", len(got), got)
	}
	if got[0].Kind != "test_failure" {
		t.Errorf("Kind = %q, want test_failure", got[0].Kind)
	}
}

// TestPytest_ParseMinSeverityWarnEmitsWarnings — an explicit
// MinSeverity=warn turns on warning emission even when
// KeepWarnings stays false. Mirrors the generic precedence rule.
func TestPytest_ParseMinSeverityWarnEmitsWarnings(t *testing.T) {
	input := `============================== warnings summary ===============================
tests/test_a.py:10
  /path/tests/test_a.py:10: UserWarning: be careful
=========================== short test summary info ============================
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := pytest.Format{}.Parse(ctx, strings.NewReader(input),
		formats.ParseOpts{MinSeverity: event.SeverityWarn})
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	if got[0].Kind != "warning" || got[0].Severity != event.SeverityWarn {
		t.Errorf("got Kind=%q Severity=%q; want warning/warn",
			got[0].Kind, got[0].Severity)
	}
}

// TestPytest_ParseMinSeverityInfoStillEmitsWarn — an explicit
// MinSeverity=info also emits warnings (the explicit floor wins
// over the no-info-emitted format default), matching the generic
// precedence rule.
func TestPytest_ParseMinSeverityInfoStillEmitsWarn(t *testing.T) {
	input := `============================== warnings summary ===============================
tests/test_a.py:10
  /path/tests/test_a.py:10: UserWarning: be careful
=========================== short test summary info ============================
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := pytest.Format{}.Parse(ctx, strings.NewReader(input),
		formats.ParseOpts{MinSeverity: event.SeverityInfo})
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1 (warning emitted under info floor): %+v", len(got), got)
	}
}
