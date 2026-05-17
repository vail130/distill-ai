package gotest_test

import (
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
)

// TestGotest_ParsePanicTopLevel — a `panic:` block emitted with no
// surrounding `--- FAIL:` (e.g. from a TestMain or init() panic, or
// `go run`) produces one Event with Kind="panic" and no test_id
// attribution.
func TestGotest_ParsePanicTopLevel(t *testing.T) {
	input := `panic: runtime error: index out of range [3] with length 2

goroutine 1 [running]:
main.main()
	/home/u/proj/main.go:42 +0x1a
exit status 2
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != "panic" {
		t.Errorf("kind = %q, want panic", ev.Kind)
	}
	if ev.Severity != event.SeverityError {
		t.Errorf("severity = %q, want error", ev.Severity)
	}
	if !strings.Contains(ev.Title, "runtime error: index out of range") {
		t.Errorf("title = %q; want the panic message", ev.Title)
	}
	if _, ok := ev.Metadata["test_id"]; ok {
		t.Errorf("metadata.test_id present (%q); want absent for top-level panic",
			ev.Metadata["test_id"])
	}
	// Body must include the goroutine dump.
	bodyJoined := strings.Join(ev.Body, "\n")
	if !strings.Contains(bodyJoined, "goroutine 1") {
		t.Errorf("body missing goroutine header; got %s", bodyJoined)
	}
	if !strings.Contains(bodyJoined, "main.main()") {
		t.Errorf("body missing main.main() frame; got %s", bodyJoined)
	}
}

// TestGotest_ParsePanicInsideFailure — a panic that fires inside a
// test goroutine attributes to that test via metadata.test_id, and
// the surrounding --- FAIL: block does not also emit its own
// test_failure Event (the panic wins).
func TestGotest_ParsePanicInsideFailure(t *testing.T) {
	input := `=== RUN   TestPanicky
panic: runtime error: invalid memory address [recovered]
	panic: runtime error: invalid memory address

goroutine 7 [running]:
testing.tRunner.func1(0xc000123)
	/usr/local/go/src/testing/testing.go:1234 +0x1a
panic({0x100, 0x200})
	/usr/local/go/src/runtime/panic.go:1234 +0x1b
example.com/foo.TestPanicky.func1(...)
	/home/u/proj/foo_test.go:42
--- FAIL: TestPanicky (0.00s)
FAIL
FAIL	example.com/foo	0.123s
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1 (panic suppresses test_failure): %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != "panic" {
		t.Errorf("kind = %q, want panic", ev.Kind)
	}
	if ev.Metadata["test_id"] != "TestPanicky" {
		t.Errorf("metadata.test_id = %q, want TestPanicky", ev.Metadata["test_id"])
	}
}

// TestGotest_ParsePanicMaxLines — a synthesised 300-line goroutine
// dump caps at maxPanicLines (200) with the sentinel as the final
// Body entry and metadata.panic_truncated set.
func TestGotest_ParsePanicMaxLines(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("panic: out of range\n\n")
	sb.WriteString("goroutine 1 [running]:\n")
	for i := 0; i < 300; i++ {
		// alternating function-call + tab-indented file:line tail
		sb.WriteString("main.foo(0x1, 0x2)\n")
		sb.WriteString("\t/x/main.go:1 +0x1\n")
	}
	sb.WriteString("exit status 2\n")
	got := parseAll(t, sb.String())
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	ev := got[0]
	if len(ev.Body) != 200 {
		t.Errorf("body len = %d; want 200 (maxPanicLines cap)", len(ev.Body))
	}
	if ev.Metadata["panic_truncated"] != "true" {
		t.Errorf("metadata.panic_truncated = %q; want \"true\"",
			ev.Metadata["panic_truncated"])
	}
	if ev.Body[len(ev.Body)-1] != "... [panic block truncated]" {
		t.Errorf("last body line = %q; want sentinel", ev.Body[len(ev.Body)-1])
	}
}

// TestGotest_ParseBuildFailure — a `go vet`-style error line before
// any tests run produces one Event per matched .go:line:col:
// location with Kind="build_failure" and Column set.
func TestGotest_ParseBuildFailure(t *testing.T) {
	input := `# example.com/proj
foo.go:42:7: undefined: bar
foo.go:50:1: cannot use string as int
FAIL	example.com/proj [build failed]
`
	got := parseAll(t, input)
	if len(got) != 2 {
		t.Fatalf("got %d events; want 2: %+v", len(got), got)
	}
	for i, ev := range got {
		if ev.Kind != "build_failure" {
			t.Errorf("ev[%d].kind = %q, want build_failure", i, ev.Kind)
		}
		if ev.Severity != event.SeverityError {
			t.Errorf("ev[%d].severity = %q, want error", i, ev.Severity)
		}
		if ev.Location == nil {
			t.Errorf("ev[%d].location nil; want set", i)
			continue
		}
		if ev.Location.File != "foo.go" {
			t.Errorf("ev[%d].location.file = %q, want foo.go", i, ev.Location.File)
		}
		if ev.Location.Column == nil {
			t.Errorf("ev[%d].location.column nil; want set", i)
		}
	}
	if got[0].Location.Line != 42 || got[1].Location.Line != 50 {
		t.Errorf("lines = %d, %d; want 42, 50", got[0].Location.Line, got[1].Location.Line)
	}
	if got[0].Title != "undefined: bar" {
		t.Errorf("first title = %q; want assertion message", got[0].Title)
	}
}

// TestGotest_ParseBuildFailureNotMistakenForLocation — a Go build
// error in the assertion-line slot inside a failure body still
// emits as build_failure because we match the buildErrorLinePattern
// before falling into the failure-body accumulator.
//
// Adversarial input: gotest never emits this shape inside a real
// failure block, but the parser must remain unambiguous.
func TestGotest_ParseBuildFailureLineEmitsImmediately(t *testing.T) {
	input := `foo.go:1:1: undefined: x
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	if got[0].Kind != "build_failure" {
		t.Errorf("kind = %q, want build_failure", got[0].Kind)
	}
}
