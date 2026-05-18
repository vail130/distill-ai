package pytest_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/pytest"
	"github.com/vail130/distill-ai/internal/testutil"
)

// drain reads every Event the channel emits and returns them in
// emission order. Helper for the parse tests so per-test code
// stays focused on assertions.
func drain(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// runParse is the standard parser-test harness: parses input
// against a 5s-deadline context, drains every Event, returns the
// slice. Fails the test if Parse itself errors.
func runParse(t *testing.T, input string) []event.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := pytest.Format{}.Parse(ctx, strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse err = %v, want nil", err)
	}
	return drain(ch)
}

const singleFailureInput = `============================= test session starts ==============================
platform darwin -- Python 3.11.4, pytest-7.4.0, pluggy-1.2.0
collected 1 item

tests/test_auth.py F                                                    [100%]

=================================== FAILURES ===================================
_______________________________ test_login_redirect _______________________________

    def test_login_redirect():
        creds = {"u": "alice", "p": "secret"}
        response = client.post("/login", data=creds)
        assert response.status_code == 302
>       assert response.headers["location"] == "/dashboard"
E       AssertionError: expected '/dashboard', got '/login?next=/'

tests/test_auth.py:47: AssertionError
=========================== short test summary info ============================
FAILED tests/test_auth.py::test_login_redirect - AssertionError
========================= 1 failed in 0.42s ==========================
`

// TestPytest_ParseSingleFailure — the integration fixture and its
// expected Event shape. Title is derived from the `E   ` line.
// Location is parsed from the `path:line:` summary line.
func TestPytest_ParseSingleFailure(t *testing.T) {
	got := runParse(t, singleFailureInput)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Severity != event.SeverityError {
		t.Errorf("Severity = %q, want error", ev.Severity)
	}
	if ev.Kind != "test_failure" {
		t.Errorf("Kind = %q, want test_failure", ev.Kind)
	}
	wantTitle := "AssertionError: expected '/dashboard', got '/login?next=/'"
	if ev.Title != wantTitle {
		t.Errorf("Title = %q\nwant %q", ev.Title, wantTitle)
	}
	if ev.Location == nil {
		t.Fatalf("Location is nil; want tests/test_auth.py:47")
	}
	if ev.Location.File != "tests/test_auth.py" || ev.Location.Line != 47 {
		t.Errorf("Location = %+v, want {tests/test_auth.py 47}", ev.Location)
	}
	if ev.Metadata["test_id"] != "test_login_redirect" {
		t.Errorf("metadata.test_id = %q, want test_login_redirect", ev.Metadata["test_id"])
	}
}

const multiFailureInput = `=================================== FAILURES ===================================
______________________________ test_one ______________________________

>       assert 1 == 2
E       AssertionError: one

tests/test_a.py:10: AssertionError
______________________________ test_two ______________________________

>       assert "a" == "b"
E       AssertionError: two

tests/test_a.py:20: AssertionError
=========================== short test summary info ============================
`

// TestPytest_ParseMultiFailure — two `___ test_id ___` headers in
// the same FAILURES section. Each emits a distinct Event with its
// own test_id and Location.
func TestPytest_ParseMultiFailure(t *testing.T) {
	got := runParse(t, multiFailureInput)
	if len(got) != 2 {
		t.Fatalf("got %d events; want 2: %+v", len(got), got)
	}
	if got[0].Metadata["test_id"] != "test_one" {
		t.Errorf("first test_id = %q, want test_one", got[0].Metadata["test_id"])
	}
	if got[1].Metadata["test_id"] != "test_two" {
		t.Errorf("second test_id = %q, want test_two", got[1].Metadata["test_id"])
	}
	if got[0].Location == nil || got[0].Location.Line != 10 {
		t.Errorf("first Location.Line = %+v, want 10", got[0].Location)
	}
	if got[1].Location == nil || got[1].Location.Line != 20 {
		t.Errorf("second Location.Line = %+v, want 20", got[1].Location)
	}
}

const cleanInput = `============================= test session starts ==============================
collected 2 items

tests/test_a.py ..                                                      [100%]

========================== 2 passed in 0.01s ===================================
`

// TestPytest_ParseSkipsPassing — a clean run with no FAILURES
// banner emits zero Events. The scanner discards every line in
// stateSession.
func TestPytest_ParseSkipsPassing(t *testing.T) {
	got := runParse(t, cleanInput)
	if len(got) != 0 {
		t.Fatalf("got %d events on clean input; want 0: %+v", len(got), got)
	}
}

// TestPytest_ParseParametrised — pytest emits subtest IDs of the
// form `test_name[case_a]` in the underlined header. The scanner
// captures the whole bracketed form verbatim.
func TestPytest_ParseParametrised(t *testing.T) {
	input := `=================================== FAILURES ===================================
________________ test_login[case_a-302] ________________

>       assert resp.code == 302
E       AssertionError

tests/test_auth.py:15: AssertionError
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	if got[0].Metadata["test_id"] != "test_login[case_a-302]" {
		t.Errorf("test_id = %q, want test_login[case_a-302]", got[0].Metadata["test_id"])
	}
}

// TestPytest_ParseTitleFallsBackToTestID — a failure block without
// an `E   ` line (e.g. a Python exception with no assertrewrite
// detail) falls back to the test ID for the Title rather than
// emitting an empty string.
func TestPytest_ParseTitleFallsBackToTestID(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_thing ______________________________

    def test_thing():
        raise RuntimeError

tests/x.py:3: RuntimeError
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	if got[0].Title != "test_thing" {
		t.Errorf("Title = %q, want test_thing (fallback)", got[0].Title)
	}
}

// TestPytest_ParseLocationRequiresPath — a line that looks like
// `host:port: ...` (no `/`, no `.py`) does not get picked up as a
// Location.
func TestPytest_ParseLocationRequiresPath(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_x ______________________________

>       assert connect("db", 5432)
E       AssertionError: connection refused

db:5432: connection refused
=========================== short test summary info ============================
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	if got[0].Location != nil {
		t.Errorf("Location = %+v, want nil (db:5432 is not a path)", got[0].Location)
	}
}

// TestPytest_ParseEofWithoutSummary — a truncated stream that
// ends mid-failure-block still flushes the in-flight Event so
// the data isn't lost.
func TestPytest_ParseEofWithoutSummary(t *testing.T) {
	input := `=================================== FAILURES ===================================
______________________________ test_x ______________________________

>       assert False
E       AssertionError: nope
`
	got := runParse(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events on truncated input; want 1", len(got))
	}
	if got[0].Title != "AssertionError: nope" {
		t.Errorf("Title = %q, want AssertionError: nope", got[0].Title)
	}
}

// TestPytest_ParseDeterministic — running the parser twice on the
// same input emits byte-equal Event slices. The format-wide
// determinism invariant the project enforces via property tests
// across all formats.
func TestPytest_ParseDeterministic(t *testing.T) {
	a := runParse(t, singleFailureInput)
	b := runParse(t, singleFailureInput)
	if len(a) != len(b) {
		t.Fatalf("event count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Title != b[i].Title || a[i].Kind != b[i].Kind {
			t.Errorf("event %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// TestPytest_ParseStreaming — the scanner emits each Event as soon
// as its block terminates, not after EOF. Uses testutil.SlowReader
// to drip the input through a low-bandwidth Reader and asserts
// the first Event arrives before the source closes.
func TestPytest_ParseStreaming(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := &testutil.SlowReader{
		Inner:      bytes.NewReader([]byte(multiFailureInput)),
		ChunkSize:  32,
		ChunkDelay: 5 * time.Millisecond,
	}
	ch, err := pytest.Format{}.Parse(ctx, r, formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	start := time.Now()
	ev, ok := <-ch
	if !ok {
		t.Fatal("channel closed before any Event arrived")
	}
	firstArrival := time.Since(start)
	// Drain the rest so the goroutine exits cleanly.
	drained := 0
	for range ch {
		drained++
	}
	_ = drained
	totalLen := len(multiFailureInput)
	minTotalDrip := time.Duration(totalLen/32) * 5 * time.Millisecond
	if firstArrival >= minTotalDrip {
		t.Errorf("first Event arrived after %v; SlowReader needed at least %v for full input — streaming is not happening",
			firstArrival, minTotalDrip)
	}
	if ev.Kind != "test_failure" {
		t.Errorf("first Event Kind = %q, want test_failure", ev.Kind)
	}
}

// TestPytest_ParseContextCancellation — cancelling the context
// mid-scan causes the scanner goroutine to exit promptly without
// leaking. Hard to assert "no leak" directly; we instead assert
// the channel closes within a short deadline once the context is
// cancelled.
func TestPytest_ParseContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel before reading any Event so the scanner sees ctx.Err
	// on its first iteration.
	cancel()
	ch, err := pytest.Format{}.Parse(ctx, strings.NewReader(singleFailureInput), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	done := make(chan struct{})
	go func() {
		drained := 0
		for range ch {
			drained++
		}
		_ = drained
		close(done)
	}()
	select {
	case <-done:
		// channel drained cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("scanner did not exit within 2s after context cancellation")
	}
}
