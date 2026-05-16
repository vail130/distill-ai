package pipeline

import (
	"context"
	"testing"

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
