package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// drainStageOutput consumes a channel until close, returning the
// Events in arrival order.
func drainStageOutput(t *testing.T, ch <-chan event.Event) []event.Event {
	t.Helper()
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// makeStream synthesises N error Events on a fresh buffered
// channel, then closes it. Used to drive MaxEventsStage without
// running a full pipeline.
func makeStream(n int) <-chan event.Event {
	ch := make(chan event.Event, n)
	for i := 0; i < n; i++ {
		ch <- event.Event{
			Severity: event.SeverityError,
			Kind:     "test_failure",
			Title:    "event " + string(rune('A'+i%26)),
		}
	}
	close(ch)
	return ch
}

// TestMaxEventsStage_StopsAtCap: a 10-event stream into a stage
// with Limit=3 produces exactly 3 events.
func TestMaxEventsStage_StopsAtCap(t *testing.T) {
	stage := pipeline.MaxEventsStage{Limit: 3}
	in := makeStream(10)
	ctx := context.Background()
	out := stage.Run(ctx, in)
	got := drainStageOutput(t, out)
	if len(got) != 3 {
		t.Errorf("emitted = %d, want 3", len(got))
	}
}

// TestMaxEventsStage_ExactlyAtLimit: a stream of exactly Limit
// events passes through unchanged, and the channel closes.
func TestMaxEventsStage_ExactlyAtLimit(t *testing.T) {
	stage := pipeline.MaxEventsStage{Limit: 5}
	in := makeStream(5)
	out := stage.Run(context.Background(), in)
	got := drainStageOutput(t, out)
	if len(got) != 5 {
		t.Errorf("emitted = %d, want 5", len(got))
	}
}

// TestMaxEventsStage_ZeroDisablesCap: Limit <= 0 forwards every
// Event. Build is expected to omit the stage in that case, but
// the runtime check exists so direct field-level construction
// doesn't surprise callers.
func TestMaxEventsStage_ZeroDisablesCap(t *testing.T) {
	stage := pipeline.MaxEventsStage{Limit: 0}
	in := makeStream(7)
	out := stage.Run(context.Background(), in)
	got := drainStageOutput(t, out)
	if len(got) != 7 {
		t.Errorf("emitted = %d, want 7 (no cap)", len(got))
	}
}

// TestMaxEventsStage_NegativeDisablesCap mirrors the zero case
// for negative inputs.
func TestMaxEventsStage_NegativeDisablesCap(t *testing.T) {
	stage := pipeline.MaxEventsStage{Limit: -5}
	in := makeStream(3)
	out := stage.Run(context.Background(), in)
	got := drainStageOutput(t, out)
	if len(got) != 3 {
		t.Errorf("emitted = %d, want 3", len(got))
	}
}

// TestMaxEventsStage_ContextCancellation: cancelling ctx
// mid-stream closes the output channel within a bounded time
// and does not leak the drain goroutine.
func TestMaxEventsStage_ContextCancellation(t *testing.T) {
	// A slow producer so the cancel can land mid-stream.
	in := make(chan event.Event)
	go func() {
		defer close(in)
		for i := 0; i < 100; i++ {
			in <- event.Event{Title: "x"}
			time.Sleep(time.Millisecond)
		}
	}()
	stage := pipeline.MaxEventsStage{Limit: 50}
	ctx, cancel := context.WithCancel(context.Background())
	out := stage.Run(ctx, in)
	// Read one Event so we know the stage is running.
	<-out
	cancel()
	done := make(chan struct{})
	go func() {
		//revive:disable-next-line:empty-block
		for range out { //nolint:revive // empty drain
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("channel did not close within 2s after cancel")
	}
}

// TestBuild_OmitsMaxEventsStageWhenLimitZero: Build does not
// include a MaxEventsStage when MaxEvents is zero. Asserted via
// Pipeline.Stages length.
func TestBuild_OmitsMaxEventsStageWhenLimitZero(t *testing.T) {
	src := &stubSource{}
	sink := &countSink{}
	p, err := pipeline.Build(src, sink, pipeline.Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, st := range p.Stages {
		if _, ok := st.(pipeline.MaxEventsStage); ok {
			t.Errorf("MaxEventsStage present despite MaxEvents=0")
		}
	}
}

// TestBuild_IncludesMaxEventsStageWhenLimitPositive: Build
// appends a MaxEventsStage after BudgetStage when MaxEvents > 0.
func TestBuild_IncludesMaxEventsStageWhenLimitPositive(t *testing.T) {
	src := &stubSource{}
	sink := &countSink{}
	p, err := pipeline.Build(src, sink, pipeline.Options{MaxEvents: 5})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var found bool
	for _, st := range p.Stages {
		if _, ok := st.(pipeline.MaxEventsStage); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("MaxEventsStage missing despite MaxEvents=5")
	}
}

// TestBuild_MaxEventsRunsAfterBudgetStage: when both Budget and
// MaxEvents are set, the order is Budget → MaxEvents. The order
// matters because BudgetStage's severity-priority sort needs the
// full event set; MaxEventsStage then trims to N.
func TestBuild_MaxEventsRunsAfterBudgetStage(t *testing.T) {
	src := &stubSource{}
	sink := &countSink{}
	p, err := pipeline.Build(src, sink, pipeline.Options{
		Budget:    100,
		Tokenizer: "heuristic",
		MaxEvents: 3,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var budgetIdx, maxIdx = -1, -1
	for i, st := range p.Stages {
		if _, ok := st.(pipeline.BudgetStage); ok {
			budgetIdx = i
		}
		if _, ok := st.(pipeline.MaxEventsStage); ok {
			maxIdx = i
		}
	}
	if budgetIdx < 0 || maxIdx < 0 {
		t.Fatalf("missing stages: budget=%d, max=%d", budgetIdx, maxIdx)
	}
	if budgetIdx >= maxIdx {
		t.Errorf("stage order wrong: budget at %d, max at %d", budgetIdx, maxIdx)
	}
}

// stubSource emits no events and never errors. Used by
// stage-order tests that don't actually run the pipeline.
type stubSource struct{}

func (stubSource) Source(_ context.Context) (<-chan event.Event, error) {
	ch := make(chan event.Event)
	close(ch)
	return ch, nil
}

// countSink consumes every Event silently.
type countSink struct{ n int }

func (s *countSink) Sink(_ context.Context, in <-chan event.Event) error {
	for range in {
		s.n++
	}
	return nil
}
