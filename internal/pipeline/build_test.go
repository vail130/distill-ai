package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
)

// mustBuild fails the test on a Build error. Used everywhere the
// fixture is known-valid so the test body isn't littered with the
// error-handling boilerplate that does not exercise the unit.
func mustBuild(t *testing.T, src Source, sink Sink, opts Options) *Pipeline {
	t.Helper()
	p, err := Build(src, sink, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return p
}

func TestBuild_DefaultsAreSafe(t *testing.T) {
	events := []event.Event{
		{Title: "a"}, {Title: "a"}, {Title: "b"},
	}
	src := &channelSource{events: events, buf: 4}
	sink := &collectSink{}
	p := mustBuild(t, src, sink, Options{})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// DedupeWindow=0: every event passes through.
	if len(sink.got) != 3 {
		t.Fatalf("got %d events, want 3 (dedupe should be off)", len(sink.got))
	}
	for _, ev := range sink.got {
		if ev.Count != 1 {
			t.Errorf("Count=%d, want 1 with DedupeWindow=0", ev.Count)
		}
	}
}

func TestBuild_DedupeAndCollapseChainTogether(t *testing.T) {
	frames := []event.StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3.11/site-packages/x.py"},
		{File: "/lib/python3.11/site-packages/y.py"},
		{File: "app/handler.py"},
	}
	ev := event.Event{
		Title:    "boom",
		Location: &event.Location{File: "app/main.py", Line: 1},
		Frames:   frames,
	}
	events := []event.Event{ev, ev, ev, ev}
	src := &channelSource{events: events, buf: 4}
	sink := &collectSink{}
	p := mustBuild(t, src, sink, Options{DedupeWindow: 8})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 1 {
		t.Fatalf("got %d events, want 1", len(sink.got))
	}
	out := sink.got[0]
	if out.Count != 4 {
		t.Errorf("Count=%d, want 4", out.Count)
	}
	if out.FramesCollapsed != 2 {
		t.Errorf("FramesCollapsed=%d, want 2", out.FramesCollapsed)
	}
	if len(out.Frames) != 2 {
		t.Errorf("len(Frames)=%d, want 2 after collapse", len(out.Frames))
	}
}

func TestBuild_StageOrder_CollapseBeforeDedupe(t *testing.T) {
	p := mustBuild(t, &channelSource{}, &collectSink{}, Options{})
	if len(p.Stages) != 2 {
		t.Fatalf("got %d stages, want 2", len(p.Stages))
	}
	if _, ok := p.Stages[0].(CollapseStage); !ok {
		t.Errorf("stage 0 = %T, want CollapseStage", p.Stages[0])
	}
	if _, ok := p.Stages[1].(DedupeStage); !ok {
		t.Errorf("stage 1 = %T, want DedupeStage", p.Stages[1])
	}
}

func TestBuild_OptionsForwarded(t *testing.T) {
	p := mustBuild(t, &channelSource{}, &collectSink{}, Options{
		DedupeWindow: 99,
		KeepVendor:   true,
		BufferSize:   42,
	})
	if p.BufferSize != 42 {
		t.Errorf("BufferSize=%d, want 42", p.BufferSize)
	}
	collapse, ok := p.Stages[0].(CollapseStage)
	if !ok || !collapse.KeepVendor {
		t.Errorf("Stages[0]=%+v, want CollapseStage{KeepVendor:true}", p.Stages[0])
	}
	dedupe, ok := p.Stages[1].(DedupeStage)
	if !ok || dedupe.Window != 99 {
		t.Errorf("Stages[1]=%+v, want DedupeStage{Window:99}", p.Stages[1])
	}
}

func TestBuild_KeepVendorPreservesFramesEndToEnd(t *testing.T) {
	frames := []event.StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3.11/site-packages/x.py"},
		{File: "app/handler.py"},
	}
	ev := event.Event{Title: "boom", Frames: frames}
	src := &channelSource{events: []event.Event{ev}, buf: 1}
	sink := &collectSink{}
	p := mustBuild(t, src, sink, Options{KeepVendor: true, DedupeWindow: 0})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 1 {
		t.Fatalf("got %d events, want 1", len(sink.got))
	}
	if got := sink.got[0]; len(got.Frames) != 3 || got.FramesCollapsed != 0 {
		t.Errorf("KeepVendor: Frames=%+v FramesCollapsed=%d, want 3/0", got.Frames, got.FramesCollapsed)
	}
	if !sink.got[0].Frames[1].Vendor {
		t.Error("vendor frame should still carry Vendor=true under KeepVendor")
	}
}

func TestBuild_BudgetZeroOmitsBudgetStage(t *testing.T) {
	p := mustBuild(t, &channelSource{}, &collectSink{}, Options{})
	if len(p.Stages) != 2 {
		t.Fatalf("got %d stages, want 2 (no BudgetStage with Budget=0)", len(p.Stages))
	}
	for i, st := range p.Stages {
		if _, ok := st.(BudgetStage); ok {
			t.Errorf("stage %d is BudgetStage; should be omitted when Budget=0", i)
		}
	}
	if p.BudgetCounters != nil {
		t.Errorf("BudgetCounters=%+v, want nil when Budget=0", p.BudgetCounters)
	}
}

func TestBuild_BudgetSetIncludesBudgetStage(t *testing.T) {
	p := mustBuild(t, &channelSource{}, &collectSink{}, Options{Budget: 100})
	if len(p.Stages) != 3 {
		t.Fatalf("got %d stages, want 3 (Collapse, Dedupe, Budget)", len(p.Stages))
	}
	if _, ok := p.Stages[0].(CollapseStage); !ok {
		t.Errorf("stage 0 = %T, want CollapseStage", p.Stages[0])
	}
	if _, ok := p.Stages[1].(DedupeStage); !ok {
		t.Errorf("stage 1 = %T, want DedupeStage", p.Stages[1])
	}
	budget, ok := p.Stages[2].(BudgetStage)
	if !ok {
		t.Fatalf("stage 2 = %T, want BudgetStage", p.Stages[2])
	}
	if budget.Budget != 100 {
		t.Errorf("BudgetStage.Budget=%d, want 100", budget.Budget)
	}
	if budget.Estimator == nil {
		t.Error("BudgetStage.Estimator is nil")
	}
	if p.BudgetCounters == nil {
		t.Fatal("BudgetCounters is nil; should be populated when Budget>0")
	}
	if budget.Counters != p.BudgetCounters {
		t.Error("BudgetStage.Counters and Pipeline.BudgetCounters refer to different values")
	}
}

func TestBuild_BudgetCountersExposed(t *testing.T) {
	events := []event.Event{
		{Severity: event.SeverityError, Title: "e1", Body: []string{"body"}},
		{Severity: event.SeverityError, Title: "e2", Body: []string{"body"}},
	}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	p := mustBuild(t, src, sink, Options{Budget: 200})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if p.BudgetCounters == nil {
		t.Fatal("BudgetCounters is nil after Run")
	}
	if p.BudgetCounters.EventsBuffered != 2 {
		t.Errorf("EventsBuffered=%d, want 2", p.BudgetCounters.EventsBuffered)
	}
	if p.BudgetCounters.EventsEmitted != 2 {
		t.Errorf("EventsEmitted=%d, want 2", p.BudgetCounters.EventsEmitted)
	}
}

func TestBuild_UnknownTokenizerErrors(t *testing.T) {
	_, err := Build(&channelSource{}, &collectSink{}, Options{
		Budget:    100,
		Tokenizer: "ggml",
	})
	if err == nil {
		t.Fatal("Build with unknown tokenizer returned nil error")
	}
}

func TestBuild_TokenizerHeuristicByDefault(t *testing.T) {
	// Empty Tokenizer string and Budget>0 must succeed (heuristic
	// is the default; no network or vocab init required).
	p := mustBuild(t, &channelSource{}, &collectSink{}, Options{Budget: 100})
	budget, ok := p.Stages[2].(BudgetStage)
	if !ok {
		t.Fatalf("stage 2 = %T, want BudgetStage", p.Stages[2])
	}
	if budget.Estimator == nil {
		t.Error("Estimator is nil with empty Tokenizer")
	}
}

func TestBuild_UnknownTokenizerDoesNotStartGoroutines(t *testing.T) {
	// Build with an invalid tokenizer must not allocate any
	// goroutine: the error returns before Run is called.
	p, err := Build(&channelSource{}, &collectSink{}, Options{
		Budget:    100,
		Tokenizer: "nonsense",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if p != nil {
		t.Errorf("Build returned non-nil Pipeline with error: %+v", p)
	}
}

// TestBuild_EnvelopeSignalsNilLeavesSourceUntouched asserts that the
// fan-in wrapper is only constructed when EnvelopeSignals is
// actually populated. The bare Source flows straight through Build
// otherwise, matching the pre-M13 behaviour byte-for-byte.
func TestBuild_EnvelopeSignalsNilLeavesSourceUntouched(t *testing.T) {
	src := &channelSource{events: []event.Event{makeEvent("a")}, buf: 1}
	p := mustBuild(t, src, &collectSink{}, Options{})
	if p.Source != src {
		t.Errorf("Build wrapped Source despite nil EnvelopeSignals: got %T, want *channelSource", p.Source)
	}
}

// TestBuild_EnvelopeSignalsMergedIntoStream covers the M13.2 DoD:
// signals delivered on Options.EnvelopeSignals appear in the Sink's
// output stream alongside parser Events.
func TestBuild_EnvelopeSignalsMergedIntoStream(t *testing.T) {
	parserEvents := []event.Event{
		makeEvent("parser-1"),
		makeEvent("parser-2"),
	}
	signals := make(chan event.Event, 2)
	signals <- event.Event{Title: "envelope-1", Kind: "envelope_error"}
	signals <- event.Event{Title: "envelope-2", Kind: "envelope_warning"}
	close(signals)
	src := &channelSource{events: parserEvents, buf: 2}
	sink := &collectSink{}
	p := mustBuild(t, src, sink, Options{EnvelopeSignals: signals})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 4 {
		t.Fatalf("got %d events from Sink, want 4 (2 parser + 2 envelope)", len(sink.got))
	}
	titles := map[string]bool{}
	kinds := map[string]bool{}
	for _, ev := range sink.got {
		titles[ev.Title] = true
		kinds[ev.Kind] = true
	}
	for _, want := range []string{"parser-1", "parser-2", "envelope-1", "envelope-2"} {
		if !titles[want] {
			t.Errorf("expected Title %q in Sink output; got titles %v", want, titlesOf(sink.got))
		}
	}
	if !kinds["envelope_error"] || !kinds["envelope_warning"] {
		t.Errorf("envelope Kinds did not survive the fan-in: kinds=%v", kinds)
	}
}

// TestBuild_EnvelopeSignalsClosedFirstStillDrainsParser asserts that
// closing the signals channel before the parser finishes does NOT
// terminate the pipeline early. The wrapper must continue draining
// the parser's stream until it closes too.
func TestBuild_EnvelopeSignalsClosedFirstStillDrainsParser(t *testing.T) {
	parserEvents := []event.Event{
		makeEvent("p1"), makeEvent("p2"), makeEvent("p3"),
	}
	signals := make(chan event.Event)
	close(signals) // closed before any parser event arrives
	src := &channelSource{events: parserEvents, buf: 3}
	sink := &collectSink{}
	p := mustBuild(t, src, sink, Options{EnvelopeSignals: signals})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 3 {
		t.Fatalf("got %d events, want 3 (all parser events should arrive even after signals close)", len(sink.got))
	}
}

// TestBuild_EnvelopeSignalsParserClosedFirstStillDrainsSignals is the
// mirror image of the above: when the parser finishes first, the
// merger must continue forwarding signals.
func TestBuild_EnvelopeSignalsParserClosedFirstStillDrainsSignals(t *testing.T) {
	signals := make(chan event.Event, 2)
	signals <- event.Event{Title: "s1", Kind: "envelope_error"}
	signals <- event.Event{Title: "s2", Kind: "envelope_warning"}
	close(signals)
	// channelSource with no events closes immediately.
	src := &channelSource{events: nil, buf: 1}
	sink := &collectSink{}
	p := mustBuild(t, src, sink, Options{EnvelopeSignals: signals})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 2 {
		t.Fatalf("got %d events, want 2 (signals must drain after parser closes)", len(sink.got))
	}
	titles := titlesOf(sink.got)
	want := map[string]bool{"s1": true, "s2": true}
	for _, title := range titles {
		if !want[title] {
			t.Errorf("unexpected Title %q in Sink output", title)
		}
	}
}

// TestBuild_EnvelopeSignalsHonoursContextCancel asserts that the
// fan-in goroutine exits when ctx is cancelled rather than blocking
// on a signal channel that never closes.
func TestBuild_EnvelopeSignalsHonoursContextCancel(t *testing.T) {
	// signals is an unbuffered channel that we never close and
	// never write to. The merger goroutine would block forever
	// without ctx-cancel handling; this test asserts it doesn't.
	signals := make(chan event.Event)
	src := &channelSource{events: nil, buf: 1}
	sink := &collectSink{}
	p := mustBuild(t, src, sink, Options{EnvelopeSignals: signals})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		// Any error (including ctx.Err) is acceptable; the
		// important property is that Run returned.
		_ = err
	case <-time.After(1 * time.Second):
		t.Fatal("Run did not return within 1s after ctx cancel; fan-in goroutine likely leaked")
	}
}

func titlesOf(events []event.Event) []string {
	out := make([]string, 0, len(events))
	for i := range events {
		out = append(out, events[i].Title)
	}
	return out
}
