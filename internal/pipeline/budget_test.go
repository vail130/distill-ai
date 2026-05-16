package pipeline

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/tokens"
)

// budgetEvent builds an Event with the given severity, title, and
// body lines.
func budgetEvent(sev event.Severity, title string, body ...string) event.Event {
	return event.Event{
		Severity: sev,
		Title:    title,
		Body:     body,
	}
}

func TestBudgetStage_ZeroBudgetIsPassthrough(t *testing.T) {
	const n = 5
	events := make([]event.Event, n)
	for i := range events {
		events[i] = budgetEvent(event.SeverityError, "evt-"+itoa(i), "body")
	}
	src := &channelSource{events: events, buf: 2}
	sink := &collectSink{}
	counters := &BudgetCounters{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    0,
			Estimator: tokens.Default(),
			Counters:  counters,
		}},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != n {
		t.Fatalf("got %d events, want %d (Budget=0 should pass through)", len(sink.got), n)
	}
	if counters.EventsBuffered != n || counters.EventsEmitted != n {
		t.Errorf("Counters Buffered/Emitted = %d/%d, want %d/%d",
			counters.EventsBuffered, counters.EventsEmitted, n, n)
	}
	if counters.EventsDroppedBudget != 0 || counters.EventsTruncated != 0 {
		t.Errorf("Counters Dropped/Truncated = %d/%d, want 0/0",
			counters.EventsDroppedBudget, counters.EventsTruncated)
	}
}

func TestBudgetStage_EmitsHighestSeverityFirst(t *testing.T) {
	// Three events; budget allows roughly two.
	events := []event.Event{
		budgetEvent(event.SeverityInfo, "info-1", "info body line"),
		budgetEvent(event.SeverityWarn, "warn-1", "warn body line"),
		budgetEvent(event.SeverityError, "err-1", "error body line"),
	}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	counters := &BudgetCounters{}
	// Estimate two events worth, plus the reserve. The heuristic
	// estimator reports ~6 tokens per event with this content; 50
	// tokens minus 30 reserve leaves ~20 for events → 2-3 events.
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    50,
			Reserve:   30,
			Estimator: tokens.Default(),
			Counters:  counters,
		}},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) == 0 {
		t.Fatalf("budget too tight: got 0 events, want at least 1")
	}
	// The first emitted event must be the error.
	if sink.got[0].Title != "err-1" {
		t.Errorf("first emitted Title=%q, want err-1", sink.got[0].Title)
	}
	// If exactly two emitted, the second must be the warn.
	if len(sink.got) >= 2 && sink.got[1].Title != "warn-1" {
		t.Errorf("second emitted Title=%q, want warn-1", sink.got[1].Title)
	}
}

func TestBudgetStage_DropsLowestFirstByArrivalOrder(t *testing.T) {
	// Three errors, identical cost. With a tight budget that only
	// fits two, the later-arriving error drops first. Per-event
	// cost with this content is ~2 tokens via the heuristic; with
	// Reserve=10 and Budget=15 we get 5 tokens of room → 2 events.
	events := []event.Event{
		budgetEvent(event.SeverityError, "first", "body"),
		budgetEvent(event.SeverityError, "second", "body"),
		budgetEvent(event.SeverityError, "third", "body"),
	}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	counters := &BudgetCounters{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    15,
			Reserve:   10,
			Estimator: tokens.Default(),
			Counters:  counters,
		}},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 2 {
		t.Fatalf("got %d events, want 2 (budget tuned for 2)", len(sink.got))
	}
	gotTitles := []string{sink.got[0].Title, sink.got[1].Title}
	wantTitles := []string{"first", "second"}
	for i := range wantTitles {
		if gotTitles[i] != wantTitles[i] {
			t.Errorf("emitted[%d]=%q, want %q (later arrivals must drop first)", i, gotTitles[i], wantTitles[i])
		}
	}
	if counters.EventsDroppedBudget != 1 {
		t.Errorf("EventsDroppedBudget=%d, want 1", counters.EventsDroppedBudget)
	}
}

func TestBudgetStage_TruncatesSingleOversizedEvent(t *testing.T) {
	// Big body, small title. The Title alone must fit in
	// (Budget - Reserve); the full body must not.
	bigBody := []string{
		"first body line that should survive truncation",
		strings.Repeat("noise word ", 50),
		strings.Repeat("more noise ", 50),
	}
	events := []event.Event{
		{
			Severity: event.SeverityError,
			Title:    "boom",
			Body:     bigBody,
		},
	}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	counters := &BudgetCounters{}
	// 50 - 30 = 20 tokens of room. The first body line + sentinel
	// + title fits in 20 tokens; the full body doesn't.
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    50,
			Estimator: tokens.Default(),
			Counters:  counters,
		}},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 1 {
		t.Fatalf("got %d events, want 1 truncated event", len(sink.got))
	}
	out := sink.got[0]
	if !out.Truncated {
		t.Error("event must have Truncated=true")
	}
	if len(out.Body) < 1 || out.Body[0] != bigBody[0] {
		t.Errorf("first body line lost: got %+v", out.Body)
	}
	if len(out.Body) < 2 || out.Body[len(out.Body)-1] != BudgetTruncationSentinel {
		t.Errorf("sentinel missing: got %+v", out.Body)
	}
	if counters.EventsTruncated != 1 || counters.EventsEmitted != 1 {
		t.Errorf("Counters Truncated/Emitted = %d/%d, want 1/1",
			counters.EventsTruncated, counters.EventsEmitted)
	}
}

func TestBudgetStage_DropsUntruncatableEvent(t *testing.T) {
	// Title alone exceeds remaining budget after reserve. Cannot
	// be truncated to fit; must be dropped.
	hugeTitle := strings.Repeat("very-long-token-rich-title ", 30)
	events := []event.Event{
		{
			Severity: event.SeverityError,
			Title:    hugeTitle,
			Body:     []string{"body"},
		},
	}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	counters := &BudgetCounters{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    35, // 35 - 30 reserve = 5 token budget; title is many more
			Estimator: tokens.Default(),
			Counters:  counters,
		}},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 0 {
		t.Fatalf("got %d events, want 0 (untruncatable should drop)", len(sink.got))
	}
	if counters.EventsDroppedBudget != 1 {
		t.Errorf("EventsDroppedBudget=%d, want 1", counters.EventsDroppedBudget)
	}
	if counters.EventsTruncated != 0 {
		t.Errorf("EventsTruncated=%d, want 0", counters.EventsTruncated)
	}
}

func TestBudgetStage_ReserveProtected(t *testing.T) {
	// With Budget=100, Reserve=30, the stage must never emit more
	// than ~70 tokens of estimated output. Heuristic margin ±15%.
	const budget, reserve = 100, 30
	const emitCap = budget - reserve
	events := make([]event.Event, 50)
	for i := range events {
		events[i] = budgetEvent(event.SeverityError, "evt-"+itoa(i), "moderately long body line")
	}
	src := &channelSource{events: events, buf: 4}
	sink := &collectSink{}
	counters := &BudgetCounters{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    budget,
			Reserve:   reserve,
			Estimator: tokens.Default(),
			Counters:  counters,
		}},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if counters.EstimatedTokens > emitCap {
		t.Errorf("EstimatedTokens=%d, want <= %d (reserve must be protected)",
			counters.EstimatedTokens, emitCap)
	}
	if counters.EventsDroppedBudget == 0 {
		t.Error("expected some drops with 50 events and a 70-token cap")
	}
}

func TestBudgetStage_BudgetSmallerThanReserveDropsAll(t *testing.T) {
	events := []event.Event{
		budgetEvent(event.SeverityError, "a", "b"),
		budgetEvent(event.SeverityError, "b", "c"),
	}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	counters := &BudgetCounters{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    10,
			Reserve:   30,
			Estimator: tokens.Default(),
			Counters:  counters,
		}},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 0 {
		t.Fatalf("got %d events, want 0 (budget < reserve)", len(sink.got))
	}
	if counters.EventsDroppedBudget != 2 {
		t.Errorf("EventsDroppedBudget=%d, want 2", counters.EventsDroppedBudget)
	}
}

func TestBudgetStage_CountersAccurate(t *testing.T) {
	events := []event.Event{
		budgetEvent(event.SeverityError, "e1", "body"),
		budgetEvent(event.SeverityError, "e2", "body"),
		budgetEvent(event.SeverityInfo, "i1", "body"),
	}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	counters := &BudgetCounters{}
	// Budget large enough to fit all three.
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    200,
			Estimator: tokens.Default(),
			Counters:  counters,
		}},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if counters.EventsBuffered != 3 {
		t.Errorf("EventsBuffered=%d, want 3", counters.EventsBuffered)
	}
	if counters.EventsEmitted != 3 {
		t.Errorf("EventsEmitted=%d, want 3", counters.EventsEmitted)
	}
	if counters.EventsDroppedBudget != 0 {
		t.Errorf("EventsDroppedBudget=%d, want 0", counters.EventsDroppedBudget)
	}
	if counters.EventsTruncated != 0 {
		t.Errorf("EventsTruncated=%d, want 0", counters.EventsTruncated)
	}
	if counters.EstimatedTokens <= 0 {
		t.Errorf("EstimatedTokens=%d, want > 0", counters.EstimatedTokens)
	}
}

func TestBudgetStage_DeterministicOrder(t *testing.T) {
	events := []event.Event{
		budgetEvent(event.SeverityWarn, "w1", "warn body"),
		budgetEvent(event.SeverityError, "e1", "error body"),
		budgetEvent(event.SeverityInfo, "i1", "info body"),
		budgetEvent(event.SeverityError, "e2", "error body two"),
		budgetEvent(event.SeverityWarn, "w2", "warn body two"),
	}
	run := func() []string {
		src := &channelSource{events: events, buf: 1}
		sink := &collectSink{}
		p := &Pipeline{
			Source: src,
			Stages: []Stage{BudgetStage{
				Budget:    200,
				Estimator: tokens.Default(),
			}},
			Sink: sink,
		}
		if err := p.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		got := make([]string, len(sink.got))
		for i, ev := range sink.got {
			got[i] = ev.Title
		}
		return got
	}
	a := run()
	b := run()
	if len(a) != len(b) {
		t.Fatalf("two runs produced different lengths: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("position %d: run1=%q run2=%q", i, a[i], b[i])
		}
	}
	// Expected ordering: errors first (e1 before e2 by arrival),
	// then warns (w1 before w2), then info.
	want := []string{"e1", "e2", "w1", "w2", "i1"}
	for i, w := range want {
		if i >= len(a) || a[i] != w {
			t.Errorf("ordering[%d]=%q, want %q", i, a[i], w)
		}
	}
}

func TestBudgetStage_ContextCancellation(t *testing.T) {
	// Use a never-closing source so the buffered enforce path is
	// definitely still reading when cancellation fires.
	runtime.GC()
	before := runtime.NumGoroutine()
	srcCh := make(chan event.Event)
	src := readerSource{ch: srcCh}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{BudgetStage{
			Budget:    100,
			Estimator: tokens.Default(),
		}},
		Sink: sink,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Run() did not return within 2s of cancel")
	}
	close(srcCh)
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()
	if after-before > 2 {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestBudgetCounters_ForcedDropsTrueOnDrops(t *testing.T) {
	c := &BudgetCounters{EventsDroppedBudget: 1}
	if !c.ForcedDrops() {
		t.Error("ForcedDrops()=false with EventsDroppedBudget>0")
	}
}

func TestBudgetCounters_ForcedDropsTrueOnTruncations(t *testing.T) {
	c := &BudgetCounters{EventsTruncated: 1}
	if !c.ForcedDrops() {
		t.Error("ForcedDrops()=false with EventsTruncated>0")
	}
}

func TestBudgetCounters_ForcedDropsFalseOnCleanRun(t *testing.T) {
	c := &BudgetCounters{EventsBuffered: 5, EventsEmitted: 5}
	if c.ForcedDrops() {
		t.Error("ForcedDrops()=true on clean run")
	}
}

func TestBudgetCounters_ForcedDropsFalseOnNilReceiver(t *testing.T) {
	var c *BudgetCounters
	if c.ForcedDrops() {
		t.Error("ForcedDrops()=true on nil receiver")
	}
}
