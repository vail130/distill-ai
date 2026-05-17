package pipeline_test

import (
	"context"
	"sync"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
	"github.com/vail130/distill-ai/internal/tokens"
)

// TestExplainLog_AddAndEntries — basic shape: Add records entries,
// Entries returns them in Seq order.
func TestExplainLog_AddAndEntries(t *testing.T) {
	log := &pipeline.ExplainLog{}
	log.Add("budget", "first", nil, event.SeverityError)
	log.Add("budget", "second", nil, event.SeverityWarn)
	got := log.Entries()
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Title != "first" || got[1].Title != "second" {
		t.Errorf("entries in wrong order: %v", got)
	}
	if got[0].Seq != 0 || got[1].Seq != 1 {
		t.Errorf("Seq numbers wrong: %d, %d", got[0].Seq, got[1].Seq)
	}
}

// TestExplainLog_ConcurrentAdd — 100 goroutines each adding once
// produces 100 entries with distinct Seq numbers. -race covers
// the locking.
func TestExplainLog_ConcurrentAdd(t *testing.T) {
	log := &pipeline.ExplainLog{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Add("budget", "concurrent", nil, event.SeverityError)
		}()
	}
	wg.Wait()
	if log.Len() != 100 {
		t.Errorf("Len = %d, want 100", log.Len())
	}
	entries := log.Entries()
	seen := make(map[int]bool)
	for _, e := range entries {
		if seen[e.Seq] {
			t.Errorf("duplicate Seq %d", e.Seq)
		}
		seen[e.Seq] = true
	}
}

// TestExplainingBudgetStage_PassthroughWhenBudgetZero — Budget=0
// degrades to a passthrough, log stays empty.
func TestExplainingBudgetStage_PassthroughWhenBudgetZero(t *testing.T) {
	log := &pipeline.ExplainLog{}
	stage := pipeline.ExplainingBudgetStage{
		Budget:    0,
		Estimator: tokens.Default(),
		Log:       log,
	}
	in := make(chan event.Event, 3)
	for i := 0; i < 3; i++ {
		in <- event.Event{Severity: event.SeverityError, Title: "ok"}
	}
	close(in)
	out := stage.Run(context.Background(), in)
	count := 0
	for range out { //nolint:revive // counting is the loop's purpose
		count++
	}
	if count != 3 {
		t.Errorf("emitted %d events, want 3", count)
	}
	if log.Len() != 0 {
		t.Errorf("log should be empty in passthrough mode; got %d entries", log.Len())
	}
}

// TestExplainingBudgetStage_DropsLogged — tight budget drops events;
// each drop is recorded in the log with reason "budget".
func TestExplainingBudgetStage_DropsLogged(t *testing.T) {
	log := &pipeline.ExplainLog{}
	counters := &pipeline.BudgetCounters{}
	stage := pipeline.ExplainingBudgetStage{
		Budget:    1, // tiny — every event drops
		Estimator: tokens.Default(),
		Counters:  counters,
		Log:       log,
	}
	in := make(chan event.Event, 5)
	for i := 0; i < 5; i++ {
		in <- event.Event{
			Severity: event.SeverityError,
			Title:    "huge title that won't fit in any budget at all",
			Body:     []string{"another long line that makes the cost go up further"},
		}
	}
	close(in)
	out := stage.Run(context.Background(), in)
	for range out { //nolint:revive // drain channel; the loop has no body
		_ = 0
	}
	if log.Len() != 5 {
		t.Errorf("log has %d entries, want 5; counters=%+v", log.Len(), counters)
	}
	for _, e := range log.Entries() {
		if e.Reason != "budget" {
			t.Errorf("entry reason = %q, want budget", e.Reason)
		}
	}
}

// TestBuildExplain_BudgetZeroSkipsExplainingStage — no
// ExplainingBudgetStage is wired when Budget == 0; the chain is
// just CollapseStage + DedupeStage. Counters and Log stay empty.
func TestBuildExplain_BudgetZeroSkipsExplainingStage(t *testing.T) {
	log := &pipeline.ExplainLog{}
	src := &emittingSource{events: []event.Event{{Severity: event.SeverityError, Title: "evt"}}}
	sink := &collectingSink{}
	p, err := pipeline.BuildExplain(src, sink, pipeline.Options{}, log)
	if err != nil {
		t.Fatalf("BuildExplain: %v", err)
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if log.Len() != 0 {
		t.Errorf("log should be empty when Budget=0; got %d", log.Len())
	}
	if p.BudgetCounters != nil {
		t.Error("BudgetCounters should be nil when Budget=0")
	}
}

// TestBuildExplain_BudgetSetWiresExplainingStage — when Budget > 0,
// the explain pipeline includes an ExplainingBudgetStage and
// populates BudgetCounters.
func TestBuildExplain_BudgetSetWiresExplainingStage(t *testing.T) {
	log := &pipeline.ExplainLog{}
	src := &emittingSource{events: []event.Event{
		{Severity: event.SeverityError, Title: "huge title one needs lots of tokens really"},
		{Severity: event.SeverityError, Title: "huge title two needs lots of tokens really"},
	}}
	sink := &collectingSink{}
	p, err := pipeline.BuildExplain(src, sink, pipeline.Options{Budget: 1}, log)
	if err != nil {
		t.Fatalf("BuildExplain: %v", err)
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if p.BudgetCounters == nil {
		t.Fatal("BudgetCounters should not be nil")
	}
	if log.Len() == 0 {
		t.Error("log should have recorded drops")
	}
}

// emittingSource is a Source that emits a fixed slice of events
// when asked.
type emittingSource struct {
	events []event.Event
}

func (s *emittingSource) Source(ctx context.Context) (<-chan event.Event, error) {
	ch := make(chan event.Event, len(s.events))
	go func() {
		defer close(ch)
		for i := range s.events {
			select {
			case <-ctx.Done():
				return
			case ch <- s.events[i]:
			}
		}
	}()
	return ch, nil
}

// collectingSink is a Sink that drains its input into a slice.
type collectingSink struct {
	events []event.Event
}

func (s *collectingSink) Sink(ctx context.Context, in <-chan event.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return nil
			}
			s.events = append(s.events, ev)
		}
	}
}
