package generic_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
)

// TestGeneric_ParsePythonTracebackBlock — a Python traceback header
// followed by indented File frames and a final KeyError line should
// produce one Event whose Body carries every block line, whose
// Title is the final exception message, and whose Frames are
// populated.
func TestGeneric_ParsePythonTracebackBlock(t *testing.T) {
	input := `before
Traceback (most recent call last):
  File "foo.py", line 10, in main
    do_thing()
  File "bar.py", line 22, in do_thing
    raise KeyError('foo')
KeyError: 'foo'
after one
after two
`
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: titles=%v", len(got), eventTitles(got))
	}
	ev := got[0]
	if ev.Title != "KeyError: 'foo'" {
		t.Errorf("Title = %q, want \"KeyError: 'foo'\"", ev.Title)
	}
	wantBody := []string{
		"Traceback (most recent call last):",
		"  File \"foo.py\", line 10, in main",
		"    do_thing()",
		"  File \"bar.py\", line 22, in do_thing",
		"    raise KeyError('foo')",
		"KeyError: 'foo'",
	}
	if !equalSlices(ev.Body, wantBody) {
		t.Errorf("Body =\n%q\nwant\n%q", ev.Body, wantBody)
	}
	if len(ev.Frames) != 2 {
		t.Fatalf("Frames len = %d, want 2: %+v", len(ev.Frames), ev.Frames)
	}
	if ev.Frames[0].File != "foo.py" || ev.Frames[0].Line != 10 || ev.Frames[0].Function != "main" {
		t.Errorf("Frames[0] = %+v, want foo.py:10 main", ev.Frames[0])
	}
	if ev.Frames[1].File != "bar.py" || ev.Frames[1].Line != 22 || ev.Frames[1].Function != "do_thing" {
		t.Errorf("Frames[1] = %+v, want bar.py:22 do_thing", ev.Frames[1])
	}
}

// TestGeneric_ParseGoPanicBlock — a Go panic followed by a
// goroutine stack should produce one Event whose Title is the
// original panic message, whose Body contains the stack, and whose
// Frames are populated.
func TestGeneric_ParseGoPanicBlock(t *testing.T) {
	input := `running...
panic: runtime error: nil pointer dereference

goroutine 1 [running]:
main.handler(0x123, 0x456)
	/app/main.go:88 +0x42
main.main()
	/app/main.go:10 +0x20
after
`
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: titles=%v", len(got), eventTitles(got))
	}
	ev := got[0]
	if ev.Title != "panic: runtime error: nil pointer dereference" {
		t.Errorf("Title = %q, want unchanged panic message", ev.Title)
	}
	if ev.Kind != "panic" {
		t.Errorf("Kind = %q, want panic", ev.Kind)
	}
	// Stack should be in Body.
	if len(ev.Body) < 5 {
		t.Errorf("Body len = %d, want at least 5 (header + stack lines): %v", len(ev.Body), ev.Body)
	}
	// Two frames.
	if len(ev.Frames) != 2 {
		t.Fatalf("Frames len = %d, want 2: %+v", len(ev.Frames), ev.Frames)
	}
	if ev.Frames[0].File != "/app/main.go" || ev.Frames[0].Line != 88 {
		t.Errorf("Frames[0] = %+v, want /app/main.go:88", ev.Frames[0])
	}
	if ev.Frames[0].Function != "main.handler" {
		t.Errorf("Frames[0].Function = %q, want main.handler", ev.Frames[0].Function)
	}
	if ev.Frames[1].Function != "main.main" {
		t.Errorf("Frames[1].Function = %q, want main.main", ev.Frames[1].Function)
	}
}

// TestGeneric_ParseJVMTracebackBlock — a Java "Exception in thread
// 'main' pkg.SomeException: ..." header followed by indented
// `at pkg.Class.method(File.java:N)` frames anchors as Kind=traceback
// (because the JVM stack-dump shape behaves like a Python
// traceback, per the M9.3 DoD continuation patterns). Title is
// re-derived to the last non-blank Body line; Frames are
// extracted via the JVM-frame regex.
func TestGeneric_ParseJVMTracebackBlock(t *testing.T) {
	input := `Exception in thread "main" java.lang.NullPointerException: Cannot invoke "String.length()"
	at com.example.App.run(App.java:42)
	at com.example.App.main(App.java:10)
	... 5 more
after one
after two
`
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %v", len(got), eventTitles(got))
	}
	ev := got[0]
	if ev.Kind != "traceback" {
		t.Errorf("Kind = %q, want traceback (JVM stack-dump shape)", ev.Kind)
	}
	if len(ev.Body) < 3 {
		t.Errorf("Body len = %d, want at least 3 (header + frames): %v", len(ev.Body), ev.Body)
	}
	if len(ev.Frames) != 2 {
		t.Fatalf("Frames len = %d, want 2 (the two `at` lines): %+v", len(ev.Frames), ev.Frames)
	}
	if ev.Frames[0].Function != "com.example.App.run" {
		t.Errorf("Frames[0].Function = %q", ev.Frames[0].Function)
	}
	if ev.Frames[0].File != "App.java" || ev.Frames[0].Line != 42 {
		t.Errorf("Frames[0] = %+v, want App.java:42", ev.Frames[0])
	}
}

// TestGeneric_ParseBlockMaxLinesCap — feed an indented block longer
// than maxBlockLines; Body has exactly maxBlockLines entries and the
// final line is the sentinel.
func TestGeneric_ParseBlockMaxLinesCap(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("Traceback (most recent call last):\n")
	for i := 0; i < 200; i++ {
		sb.WriteString("  File \"x.py\", line 1, in f\n")
	}
	sb.WriteString("after the giant block\n")
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(sb.String()), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %v", len(got), eventTitles(got))
	}
	ev := got[0]
	if len(ev.Body) != 100 {
		t.Errorf("Body len = %d, want 100 (maxBlockLines cap)", len(ev.Body))
	}
	if ev.Body[len(ev.Body)-1] != "... [block truncated]" {
		t.Errorf("Body[-1] = %q, want truncation sentinel", ev.Body[len(ev.Body)-1])
	}
}

// TestGeneric_ParseBlockEndsOnDedent — a traceback followed by a
// non-indented log line ends the block cleanly; the non-indented
// line becomes post-context.
func TestGeneric_ParseBlockEndsOnDedent(t *testing.T) {
	input := `Traceback (most recent call last):
  File "x.py", line 1, in f
KeyError: 'foo'
2026-05-17 next log line
2026-05-17 another
2026-05-17 third
`
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	ev := got[0]
	// The block ends at "KeyError" (still indented? actually
	// "KeyError: 'foo'" has no leading whitespace, but it's a
	// traceback continuation because of the "^\s" pattern... wait,
	// no — "KeyError: 'foo'" has no leading whitespace. So the
	// block ends and "KeyError" becomes post-context. But the
	// DoD says the Title becomes "the last non-blank Body line"
	// — which would be the File frame line then.
	// Re-reading the DoD: real Python tracebacks have the
	// KeyError line at column 0 (the exception type and message
	// are not indented). So the continuation rule must keep
	// going until we hit a clearly non-traceback line.
	//
	// In this fixture "2026-05-17 next log line" is the first
	// genuine non-traceback line, so the block ends there.
	if ev.Title != "KeyError: 'foo'" {
		t.Errorf("Title = %q, want KeyError: 'foo' (last non-blank Body line)", ev.Title)
	}
}

// TestGeneric_ParsePythonTracebackTitleIsExceptionLine — anchor on
// the Traceback header, end the block on a dedent; the Title must
// be the exception message at the bottom, not the header.
func TestGeneric_ParsePythonTracebackTitleIsExceptionLine(t *testing.T) {
	input := `Traceback (most recent call last):
  File "x.py", line 1, in f
    do_thing()
ValueError: bad input
log line after
log line after 2
`
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %v", len(got), eventTitles(got))
	}
	if got[0].Title != "ValueError: bad input" {
		t.Errorf("Title = %q, want \"ValueError: bad input\"", got[0].Title)
	}
}
