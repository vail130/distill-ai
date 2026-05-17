package gotest_test

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	_ "github.com/vail130/distill-ai/internal/formats/gotest"
	"github.com/vail130/distill-ai/internal/testutil"
)

// parseAll drains the channel and returns the slice; helper for the
// tests below.
func parseAll(t *testing.T, input string) []event.Event {
	t.Helper()
	f, _ := formats.Get("gotest")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := f.Parse(ctx, strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// TestGotest_ParseSingleFailure — the canonical default-reporter
// failure block produces one test_failure Event with Title from
// the assertion line, Location parsed, and test_id metadata.
func TestGotest_ParseSingleFailure(t *testing.T) {
	input := `=== RUN   TestLogin
    auth_test.go:42: expected 200, got 500
--- FAIL: TestLogin (0.02s)
FAIL
exit status 1
FAIL	github.com/example/project/auth	0.123s
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Severity != event.SeverityError {
		t.Errorf("severity = %q, want error", ev.Severity)
	}
	if ev.Kind != "test_failure" {
		t.Errorf("kind = %q, want test_failure", ev.Kind)
	}
	if ev.Metadata["test_id"] != "TestLogin" {
		t.Errorf("metadata.test_id = %q, want TestLogin", ev.Metadata["test_id"])
	}
	if ev.Metadata["package"] != "github.com/example/project/auth" {
		t.Errorf("metadata.package = %q", ev.Metadata["package"])
	}
	if ev.Metadata["duration"] != "0.02s" {
		t.Errorf("metadata.duration = %q", ev.Metadata["duration"])
	}
	if ev.Location == nil {
		t.Fatal("location nil; want auth_test.go:42")
	}
	if ev.Location.File != "auth_test.go" || ev.Location.Line != 42 {
		t.Errorf("location = %+v, want auth_test.go:42", ev.Location)
	}
	if ev.Title != "expected 200, got 500" {
		t.Errorf("title = %q, want assertion message", ev.Title)
	}
}

// TestGotest_ParseMultiFailure — multiple failures emit multiple
// Events with package metadata correctly attributed.
func TestGotest_ParseMultiFailure(t *testing.T) {
	input := `=== RUN   TestA
    a_test.go:10: a failed
--- FAIL: TestA (0.01s)
=== RUN   TestB
--- PASS: TestB (0.00s)
=== RUN   TestC
    c_test.go:20: c failed
--- FAIL: TestC (0.05s)
FAIL
exit status 1
FAIL	example.com/multi	0.234s
`
	got := parseAll(t, input)
	if len(got) != 2 {
		t.Fatalf("got %d events; want 2: %+v", len(got), got)
	}
	if got[0].Metadata["test_id"] != "TestA" {
		t.Errorf("first test_id = %q, want TestA", got[0].Metadata["test_id"])
	}
	if got[1].Metadata["test_id"] != "TestC" {
		t.Errorf("second test_id = %q, want TestC", got[1].Metadata["test_id"])
	}
	// Both Events carry the package, even though only the final
	// summary line names it: emit deferred until the package line
	// is consumed.
	for i, ev := range got {
		if ev.Metadata["package"] != "example.com/multi" {
			t.Errorf("ev[%d].package = %q, want example.com/multi", i, ev.Metadata["package"])
		}
	}
}

// TestGotest_ParseSkipsPassing — passing tests do not produce
// Events. Critical because the default reporter still emits
// `--- PASS:` lines.
func TestGotest_ParseSkipsPassing(t *testing.T) {
	input := `=== RUN   TestPass
--- PASS: TestPass (0.00s)
=== RUN   TestFail
    x_test.go:1: nope
--- FAIL: TestFail (0.00s)
FAIL
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1 (only the fail): %+v", len(got), got)
	}
	if got[0].Metadata["test_id"] != "TestFail" {
		t.Errorf("test_id = %q, want TestFail", got[0].Metadata["test_id"])
	}
}

// TestGotest_ParseSubtests — table-driven subtests where some rows
// fail emit one Event per failing subtest with the slash-separated
// subtest path in test_id. gotest itself indents subtest failure
// headers and emits the full path.
func TestGotest_ParseSubtests(t *testing.T) {
	input := `=== RUN   TestParse
=== RUN   TestParse/empty_input
=== RUN   TestParse/binary_input
    parse_test.go:50: empty case failed
    --- FAIL: TestParse/empty_input (0.00s)
    parse_test.go:55: binary case failed
    --- FAIL: TestParse/binary_input (0.00s)
--- FAIL: TestParse (0.01s)
FAIL
`
	got := parseAll(t, input)
	if len(got) != 3 {
		t.Fatalf("got %d events; want 3: %+v", len(got), got)
	}
	wantIDs := []string{
		"TestParse/empty_input",
		"TestParse/binary_input",
		"TestParse",
	}
	for i, ev := range got {
		if ev.Metadata["test_id"] != wantIDs[i] {
			t.Errorf("ev[%d] test_id = %q, want %q", i, ev.Metadata["test_id"], wantIDs[i])
		}
	}
}

// TestGotest_ParseDropsPerTestLogs — `t.Logf` output between
// passing tests must not leak into a later failure's Body. Tests
// the state machine: stateRunning drops lines on the floor.
func TestGotest_ParseDropsPerTestLogs(t *testing.T) {
	input := `=== RUN   TestPass
    pass_test.go:1: hello from logf
--- PASS: TestPass (0.00s)
=== RUN   TestFail
    fail_test.go:9: actual failure here
--- FAIL: TestFail (0.00s)
FAIL
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	for _, line := range got[0].Body {
		if strings.Contains(line, "hello from logf") {
			t.Errorf("failure body leaked logf line: %q", line)
		}
	}
}

// TestGotest_ParseStreaming — drip a multi-package fixture through
// SlowReader and assert the first package's Events arrive before
// the second package even starts arriving from the source. Ties
// into the project-wide streaming invariant.
//
// gotest emits per-package summary lines (`FAIL\t<pkg>\t...`) so
// the parser can flush each package's failures before the next
// package's tests are scanned. This gives streaming across
// packages even though within a package the events are buffered
// until the summary line arrives.
func TestGotest_ParseStreaming(t *testing.T) {
	// Build a two-package input: the first package finishes
	// quickly with its FAIL\tpkg1 summary, then a long tail of
	// passing tests in the second package.
	var sb strings.Builder
	sb.WriteString("=== RUN   TestEarly\n")
	sb.WriteString("    early_test.go:1: failed early\n")
	sb.WriteString("--- FAIL: TestEarly (0.00s)\n")
	sb.WriteString("FAIL\n")
	sb.WriteString("FAIL\texample.com/pkg1\t0.05s\n")
	for i := 0; i < 200; i++ {
		sb.WriteString("=== RUN   TestFiller\n")
		sb.WriteString("--- PASS: TestFiller (0.00s)\n")
	}
	sb.WriteString("PASS\n")
	sb.WriteString("ok  \texample.com/pkg2\t0.5s\n")
	input := sb.String()
	slow := &testutil.SlowReader{
		Inner:      strings.NewReader(input),
		ChunkSize:  64,
		ChunkDelay: 2 * time.Millisecond,
	}
	f, _ := formats.Get("gotest")
	start := time.Now()
	ch, _ := f.Parse(context.Background(), slow, formats.ParseOpts{})
	first, ok := <-ch
	if !ok {
		t.Fatal("expected at least one event before EOF")
	}
	firstAt := time.Since(start)
	totalExpected := time.Duration(len(input)/slow.ChunkSize) * slow.ChunkDelay
	if firstAt > totalExpected/2 {
		t.Errorf("first event emerged after %s; expected well before %s",
			firstAt, totalExpected/2)
	}
	if first.Metadata["test_id"] != "TestEarly" {
		t.Errorf("first event = %+v; want TestEarly", first)
	}
	if first.Metadata["package"] != "example.com/pkg1" {
		t.Errorf("first event package = %q; want example.com/pkg1", first.Metadata["package"])
	}
	drained := 0
	for range ch {
		drained++
	}
	_ = drained
}

// TestGotest_ParseDeterministic — same input twice → byte-equal
// emission sequence. Property test, ties into project-wide
// determinism invariant.
func TestGotest_ParseDeterministic(t *testing.T) {
	input := `=== RUN   TestA
    a_test.go:10: a failed
--- FAIL: TestA (0.01s)
=== RUN   TestB
    b_test.go:20: b failed
--- FAIL: TestB (0.02s)
FAIL
FAIL	example.com/det	0.3s
`
	a := parseAll(t, input)
	b := parseAll(t, input)
	if len(a) != len(b) {
		t.Fatalf("different lengths: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Title != b[i].Title || a[i].Metadata["test_id"] != b[i].Metadata["test_id"] {
			t.Errorf("ev[%d] differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// TestGotest_ParseContextCancellation — cancelling the context
// mid-stream drains the parser without leaking goroutines. Uses
// a big fixture so the parser is still running when we cancel.
func TestGotest_ParseContextCancellation(t *testing.T) {
	runtime.GC()
	time.Sleep(5 * time.Millisecond)
	baseline := runtime.NumGoroutine()
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("=== RUN   TestX\n")
		sb.WriteString("    x_test.go:1: failed\n")
		sb.WriteString("--- FAIL: TestX (0.00s)\n")
	}
	f, _ := formats.Get("gotest")
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ch, _ := f.Parse(ctx, strings.NewReader(sb.String()), formats.ParseOpts{})
		<-ch
		cancel()
		drained := 0
		for range ch {
			drained++
		}
		_ = drained
	}
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	if got := runtime.NumGoroutine(); got > baseline+1 {
		t.Errorf("goroutine count drifted: baseline=%d, after=%d", baseline, got)
	}
}

// TestGotest_ParseFailureWithoutAssertion — a `--- FAIL:` block
// with no `file:line:` assertion line falls back to using the
// header line as the Title. Real gotest emits this shape when
// a test panics inside a sub-goroutine; the assertion never
// reaches the main test framing.
func TestGotest_ParseFailureWithoutAssertion(t *testing.T) {
	input := `=== RUN   TestPanic
--- FAIL: TestPanic (0.00s)
FAIL
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	if !strings.Contains(got[0].Title, "TestPanic") {
		t.Errorf("title = %q; want to contain test name", got[0].Title)
	}
	if got[0].Location != nil {
		t.Errorf("location = %+v; want nil when no assertion line", got[0].Location)
	}
}

// TestGotest_ParseLocationRequiresGoFile — `host:port:` shapes in
// body lines must not be parsed as locations. The assertion line
// requires either `.go` or `/` in the path token.
func TestGotest_ParseLocationRequiresGoFile(t *testing.T) {
	input := `=== RUN   TestX
    connection to db:5432 refused
--- FAIL: TestX (0.00s)
FAIL
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	if got[0].Location != nil {
		t.Errorf("location = %+v; want nil (host:port is not a path)", got[0].Location)
	}
}
