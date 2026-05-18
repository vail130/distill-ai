package pytest_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/event"
)

// TestPytest_ParseFixtureError — an ERRORS section that appears
// after FAILURES carries per-test errors (typically a fixture
// failing to set up). Each block emits Kind=test_error with
// test_id populated, mirroring the test_failure shape.
//
// In practice pytest emits ERRORS *before* FAILURES when both are
// present in the same run, but the section-order rule is "ERRORS
// before any FAILURES = collection phase", so a fixture failure
// scenario must start with FAILURES already seen.
func TestPytest_ParseFixtureError(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_unrelated ______________________________

>       assert False
E       AssertionError

tests/test_a.py:5: AssertionError
=================================== ERRORS ====================================
______________________________ ERROR at setup of test_db ______________________________

@pytest.fixture
def db():
>       raise RuntimeError("connection refused")
E       RuntimeError: connection refused

tests/conftest.py:12: RuntimeError
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 2 {
		t.Fatalf("got %d events; want 2 (failure + error): %+v", len(got), got)
	}
	if got[0].Kind != "test_failure" {
		t.Errorf("first Kind = %q, want test_failure", got[0].Kind)
	}
	if got[1].Kind != "test_error" {
		t.Errorf("second Kind = %q, want test_error", got[1].Kind)
	}
	if got[1].Metadata["test_id"] != "ERROR at setup of test_db" {
		t.Errorf("test_error id = %q", got[1].Metadata["test_id"])
	}
	if got[1].Title != "RuntimeError: connection refused" {
		t.Errorf("test_error Title = %q", got[1].Title)
	}
}

// TestPytest_ParseCollectionError — when ERRORS appears *before*
// any FAILURES section, every block under it emits as
// collection_error: tests never ran, the failures live in
// conftest.py / module-level imports. test_id is absent.
func TestPytest_ParseCollectionError(t *testing.T) {
	input := `=================================== ERRORS ====================================
______________________________ ERROR collecting tests/test_imports.py ______________________________
ImportError while importing test module '/path/tests/test_imports.py'.
Hint: make sure your test modules/packages have valid Python names.
Traceback:
tests/test_imports.py:1: in <module>
    import nonexistent
E   ModuleNotFoundError: No module named 'nonexistent'
=========================== short test summary info ============================
ERROR tests/test_imports.py
======================== 1 error in 0.05s =====================================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != "collection_error" {
		t.Errorf("Kind = %q, want collection_error", ev.Kind)
	}
	if ev.Title != "ModuleNotFoundError: No module named 'nonexistent'" {
		t.Errorf("Title = %q", ev.Title)
	}
	if ev.Severity != event.SeverityError {
		t.Errorf("Severity = %q, want error", ev.Severity)
	}
	// Collection errors don't have a single test scope; test_id
	// must be absent so consumers don't index a non-existent test.
	if _, has := ev.Metadata["test_id"]; has {
		t.Errorf("collection_error must not carry test_id; got %q", ev.Metadata["test_id"])
	}
}

// TestPytest_ParseCollectionErrorPicksUpFile — the `ERROR
// collecting <path>` underline carries the file pytest tried to
// collect. When no `path:line:` summary appears further down (a
// truncated import-time error), the Location still gets the file
// name from the header so consumers can render "open
// tests/test_x.py".
func TestPytest_ParseCollectionErrorPicksUpFile(t *testing.T) {
	input := `=================================== ERRORS ====================================
______________________________ ERROR collecting tests/test_imports.py ______________________________
E   SyntaxError: invalid syntax
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	if got[0].Location == nil {
		t.Fatalf("Location = nil; want File=tests/test_imports.py from the underline")
	}
	if got[0].Location.File != "tests/test_imports.py" {
		t.Errorf("Location.File = %q, want tests/test_imports.py", got[0].Location.File)
	}
}

// TestPytest_ParseCollectionUnderlineWinsOverSectionOrder — even
// when an ERRORS section appears *after* FAILURES (so the section-
// order rule would classify the block as test_error), an
// underline of the shape `ERROR collecting <path>` always means
// collection-phase. The per-block underline overrides the
// section-level default.
func TestPytest_ParseCollectionUnderlineWinsOverSectionOrder(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_a ______________________________
>       assert False
E       AssertionError
tests/x.py:1: AssertionError
=================================== ERRORS ====================================
______________________________ ERROR collecting tests/test_b.py ______________________________
E   SyntaxError: bad
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 2 {
		t.Fatalf("got %d events; want 2", len(got))
	}
	if got[1].Kind != "collection_error" {
		t.Errorf("second Kind = %q, want collection_error (underline overrides section order)", got[1].Kind)
	}
}

// TestPytest_ParseErrorAndFailureMix — both sections, both
// produce per-test Events with the right Kinds in source order.
func TestPytest_ParseErrorAndFailureMix(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_a ______________________________
>       assert 1 == 2
E       AssertionError
tests/x.py:5: AssertionError
=================================== ERRORS ====================================
______________________________ ERROR at setup of test_b ______________________________
>       raise ValueError("bad fixture")
E       ValueError: bad fixture
tests/conftest.py:1: ValueError
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 2 {
		t.Fatalf("got %d events; want 2", len(got))
	}
	if got[0].Kind != "test_failure" || got[1].Kind != "test_error" {
		t.Errorf("Kinds = [%q, %q], want [test_failure, test_error]",
			got[0].Kind, got[1].Kind)
	}
}
