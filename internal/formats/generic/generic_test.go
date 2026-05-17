package generic_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"

	// Side-effect import: register the generic Format under its
	// reserved name so formats.Get("generic") finds it.
	_ "github.com/vail130/distill-ai/internal/formats/generic"
)

// getGeneric is the shared lookup the tests use to obtain the
// Format value. It fatals on miss so every test fails fast if the
// side-effect import is missing.
func getGeneric(t *testing.T) formats.Format {
	t.Helper()
	f, ok := formats.Get("generic")
	if !ok {
		t.Fatal("generic format not registered; expected the side-effect import to have wired it")
	}
	return f
}

// drain reads every Event the channel emits and returns them as a
// slice. Used throughout the parser tests.
func drain(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// TestGeneric_RegisteredAtInit — importing the package for side
// effect registers the Format under "generic". Anchors the wiring
// future milestones depend on.
func TestGeneric_RegisteredAtInit(t *testing.T) {
	f := getGeneric(t)
	if f.Name() != "generic" {
		t.Errorf("Format.Name() = %q, want \"generic\"", f.Name())
	}
}

// TestGeneric_DetectFloorOnSeverityHit — a sample with even one
// severity-bearing line yields confidenceFloor (0.1). The exact
// value is asserted so reviewers see the magic number anchored.
func TestGeneric_DetectFloorOnSeverityHit(t *testing.T) {
	got := getGeneric(t).Detect([]byte("info: starting up\nERROR: thing broke\ninfo: cleanup\n"))
	if got != 0.1 {
		t.Errorf("Detect on severity-hit sample = %v, want 0.1", got)
	}
}

// TestGeneric_DetectZeroOnNonMatch — innocuous prose with no
// severity markers returns 0.0, not the floor.
func TestGeneric_DetectZeroOnNonMatch(t *testing.T) {
	got := getGeneric(t).Detect([]byte("Hello, world.\nThis is fine."))
	if got != 0 {
		t.Errorf("Detect on innocuous sample = %v, want 0", got)
	}
}

// TestGeneric_DetectBelowMinThreshold — the floor must stay below
// the detector's threshold so a specific format always wins.
func TestGeneric_DetectBelowMinThreshold(t *testing.T) {
	got := getGeneric(t).Detect([]byte("ERROR: x"))
	if got >= event.ConfidenceMinDetect {
		t.Errorf("generic confidence on hit = %v; must stay < ConfidenceMinDetect (%v) so a specific format wins ties",
			got, event.ConfidenceMinDetect)
	}
}

// -----------------------------------------------------------------
// M9.2 parser tests
// -----------------------------------------------------------------

// TestGeneric_ParseSingleError — input is five innocuous lines plus
// one ERROR plus three innocuous lines; assert one Event with
// severity=error, kind=error_line, three lines of context before,
// three after.
func TestGeneric_ParseSingleError(t *testing.T) {
	input := strings.Join([]string{
		"info one",
		"info two",
		"info three",
		"info four",
		"info five",
		"ERROR: thing broke",
		"info six",
		"info seven",
		"info eight",
	}, "\n") + "\n"
	ch, err := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Severity != event.SeverityError {
		t.Errorf("Severity = %q, want error", ev.Severity)
	}
	if ev.Kind != "error_line" {
		t.Errorf("Kind = %q, want error_line", ev.Kind)
	}
	if ev.Title != "ERROR: thing broke" {
		t.Errorf("Title = %q, want %q", ev.Title, "ERROR: thing broke")
	}
	// Pre-context: the three lines immediately before the anchor.
	wantPre := []string{"info three", "info four", "info five"}
	wantPost := []string{"info six", "info seven", "info eight"}
	want := append(append([]string{}, wantPre...), wantPost...)
	if !equalSlices(ev.Context, want) {
		t.Errorf("Context = %q\nwant %q", ev.Context, want)
	}
}

// TestGeneric_ParseMultipleEvents — three anchor lines spaced far
// enough apart that contexts don't overlap; three Events emerge.
func TestGeneric_ParseMultipleEvents(t *testing.T) {
	input := strings.Join([]string{
		"info 1", "info 2", "info 3", "info 4", "info 5",
		"ERROR: first",
		"info 6", "info 7", "info 8", "info 9", "info 10",
		"info 11", "info 12", "info 13", "info 14",
		"ERROR: second",
		"info 15", "info 16", "info 17", "info 18", "info 19",
		"info 20", "info 21", "info 22", "info 23",
		"ERROR: third",
		"info 24", "info 25", "info 26", "info 27",
	}, "\n") + "\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	titles := []string{got[0].Title, got[1].Title, got[2].Title}
	want := []string{"ERROR: first", "ERROR: second", "ERROR: third"}
	if !equalSlices(titles, want) {
		t.Errorf("titles = %q, want %q", titles, want)
	}
}

// TestGeneric_ParseOverlappingContexts — two anchor lines one apart;
// both Events emit and their Context slices may share lines.
// Documents the rule that the scanner does not deduplicate adjacent
// matches into a single Event.
func TestGeneric_ParseOverlappingContexts(t *testing.T) {
	input := strings.Join([]string{
		"info a", "info b", "info c",
		"ERROR: first",
		"ERROR: second",
		"info d", "info e", "info f",
	}, "\n") + "\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: titles=%v", len(got), eventTitles(got))
	}
	// The second anchor appears inside the first's post-context;
	// the rule says we still emit two distinct Events.
	if got[0].Title != "ERROR: first" || got[1].Title != "ERROR: second" {
		t.Errorf("titles = %q, %q; want \"ERROR: first\", \"ERROR: second\"",
			got[0].Title, got[1].Title)
	}
}

// TestGeneric_ParsePanicAndException — a Go panic line and a Python
// "Exception:" line produce one Event each with the right Kind.
func TestGeneric_ParsePanicAndException(t *testing.T) {
	input := "panic: runtime error: index out of range\n" +
		"some prose\n" +
		"Exception: KeyError 'foo'\n" +
		"more prose\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), eventTitles(got))
	}
	if got[0].Kind != "panic" {
		t.Errorf("first Kind = %q, want panic", got[0].Kind)
	}
	if got[1].Kind != "exception" {
		t.Errorf("second Kind = %q, want exception", got[1].Kind)
	}
}

// TestGeneric_ParseTracebackHeader — a Python "Traceback (most
// recent call last):" line anchors a single Event with
// Kind=traceback, severity error. The body block accumulation is
// M9.3; M9.2 only emits the header.
func TestGeneric_ParseTracebackHeader(t *testing.T) {
	input := "doing stuff\nTraceback (most recent call last):\n  File \"foo.py\", line 1\nKeyError: 'foo'\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	// M9.2: the Traceback header anchors an Event. The following
	// "KeyError:" line is NOT a catalogue hit (no "Error:" anchor
	// in the middle of the word "KeyError:"; the bracket-prefix is
	// part of \bError:). The "File \"foo.py\"" line is in
	// post-context. Expect 1 Event.
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %v", len(got), eventTitles(got))
	}
	if got[0].Kind != "traceback" {
		t.Errorf("Kind = %q, want traceback", got[0].Kind)
	}
	if got[0].Severity != event.SeverityError {
		t.Errorf("Severity = %q, want error", got[0].Severity)
	}
}

// TestGeneric_ParseExtractsLocation — ERROR foo.py:42: bad thing →
// Location{File:"foo.py", Line:42}.
func TestGeneric_ParseExtractsLocation(t *testing.T) {
	input := "ERROR ./src/foo.py:42: bad thing happened\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Location == nil {
		t.Fatalf("Location = nil; want path:line extracted")
	}
	if got[0].Location.File != "./src/foo.py" {
		t.Errorf("Location.File = %q, want ./src/foo.py", got[0].Location.File)
	}
	if got[0].Location.Line != 42 {
		t.Errorf("Location.Line = %d, want 42", got[0].Location.Line)
	}
}

// TestGeneric_ParseLocationRequiresSlash — ERROR connection to
// db:5432 refused → Location nil (no slash → not a path).
func TestGeneric_ParseLocationRequiresSlash(t *testing.T) {
	input := "ERROR connection to db:5432 refused\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Location != nil {
		t.Errorf("Location = %+v; want nil (host:port has no slash)", got[0].Location)
	}
}

// TestGeneric_ParseStripsANSIFromTitle — colour codes are removed
// from Title so it's grep-able.
func TestGeneric_ParseStripsANSIFromTitle(t *testing.T) {
	input := "\x1b[31mERROR\x1b[0m: thing broke\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Title != "ERROR: thing broke" {
		t.Errorf("Title = %q, want \"ERROR: thing broke\"", got[0].Title)
	}
}

// TestGeneric_ParseBodyKeepsANSI — the same input as the strip
// test; Body[0] retains the escape sequences so users see what
// arrived.
func TestGeneric_ParseBodyKeepsANSI(t *testing.T) {
	raw := "\x1b[31mERROR\x1b[0m: thing broke"
	input := raw + "\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 || len(got[0].Body) == 0 {
		t.Fatalf("got %d events / empty body: %+v", len(got), got)
	}
	if got[0].Body[0] != raw {
		t.Errorf("Body[0] = %q, want %q (ANSI must survive in Body)", got[0].Body[0], raw)
	}
}

// TestGeneric_ParseInfoNotEmittedV1 — info markers don't anchor
// Events in v1 (catalogue has no info entries). Documents the
// "info is empty in v1" decision.
func TestGeneric_ParseInfoNotEmittedV1(t *testing.T) {
	input := "INFO: starting server\nINFO: ready\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 0 {
		t.Errorf("got %d events, want 0 (info catalogue is empty in v1): %v", len(got), eventTitles(got))
	}
}

// TestGeneric_ParseDeterministic — same input twice → byte-equal
// sequence of Events. Property test that ties into the project's
// determinism invariant.
func TestGeneric_ParseDeterministic(t *testing.T) {
	input := strings.Repeat("info\nERROR: a\nwarn: b\nWARN: c\n", 10)
	run := func() []event.Event {
		ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
		return drain(ch)
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("event count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Title != b[i].Title || a[i].Kind != b[i].Kind || a[i].Severity != b[i].Severity {
			t.Errorf("event %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// TestGeneric_ParseContextCancellation — cancel mid-stream; parser
// drains and exits. The send loop honours ctx so cancellation
// propagates even when no goroutine reads from the output channel.
func TestGeneric_ParseContextCancellation(t *testing.T) {
	input := strings.Repeat("ERROR: x\n", 1000)
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := getGeneric(t).Parse(ctx, strings.NewReader(input), formats.ParseOpts{})
	// Read exactly one event, then cancel.
	first, ok := <-ch
	if !ok {
		t.Fatal("expected at least one event before cancellation")
	}
	_ = first
	cancel()
	// Drain the rest; the goroutine must close the channel
	// promptly. Count consumed events only to give the linter
	// something to look at; the assertion is implicit (channel
	// must eventually close).
	consumed := 0
	for range ch {
		consumed++
	}
	_ = consumed
}

// TestGeneric_ParseCustomContextLines — opts.ContextLines = 1
// shortens the pre/post window to one line each.
func TestGeneric_ParseCustomContextLines(t *testing.T) {
	input := "a\nb\nc\nERROR: x\nd\ne\nf\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{ContextLines: 1})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	want := []string{"c", "d"}
	if !equalSlices(got[0].Context, want) {
		t.Errorf("Context = %q, want %q", got[0].Context, want)
	}
}

// TestGeneric_ParseEOFFlushesPendingEvent — an anchor near EOF
// emits whatever post-context lines are available, even if fewer
// than contextLines.
func TestGeneric_ParseEOFFlushesPendingEvent(t *testing.T) {
	input := "info\nERROR: at-eof\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Title != "ERROR: at-eof" {
		t.Errorf("Title = %q", got[0].Title)
	}
}

// -----------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eventTitles(evs []event.Event) []string {
	out := make([]string, len(evs))
	for i := range evs {
		out[i] = evs[i].Title
	}
	return out
}
