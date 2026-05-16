package pipeline

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
)

// channelSource adapts a slice of Events into a Source.
type channelSource struct {
	events []event.Event
	buf    int
}

func (s *channelSource) Source(ctx context.Context) (<-chan event.Event, error) {
	out := make(chan event.Event, s.buf)
	go func() {
		defer close(out)
		for i := range s.events {
			select {
			case <-ctx.Done():
				return
			case out <- s.events[i]:
			}
		}
	}()
	return out, nil
}

// collectSink stores every Event the pipeline emits.
type collectSink struct {
	got []event.Event
}

func (c *collectSink) Sink(ctx context.Context, in <-chan event.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return nil
			}
			c.got = append(c.got, ev)
		}
	}
}

func makeEvent(title string) event.Event {
	return event.Event{
		Title:    title,
		Location: &event.Location{File: "f.go", Line: 1},
		Body:     []string{"line"},
	}
}

func TestDedupeStage_PassthroughDistinct(t *testing.T) {
	const n = 50
	events := make([]event.Event, n)
	titles := make([]string, n)
	for i := range events {
		t := "evt-" + itoa(i)
		events[i] = makeEvent(t)
		titles[i] = t
	}
	src := &channelSource{events: events, buf: 8}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{DedupeStage{Window: n}},
		Sink:   sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(sink.got) != n {
		t.Fatalf("got %d events, want %d", len(sink.got), n)
	}
	for i, ev := range sink.got {
		if ev.Title != titles[i] {
			t.Fatalf("event %d Title=%q, want %q (order preserved?)", i, ev.Title, titles[i])
		}
		if ev.Count != 1 {
			t.Fatalf("event %d Count=%d, want 1", i, ev.Count)
		}
	}
}

func TestDedupeStage_DeduplicatesIdentical(t *testing.T) {
	// 8 distinct titles plus 12 copies of "dup" = 20 inputs, 9
	// unique signatures, dup Count should be 12. Window large
	// enough that no eviction happens before flush.
	events := []event.Event{
		makeEvent("a"), makeEvent("b"), makeEvent("c"),
		makeEvent("dup"), makeEvent("dup"), makeEvent("dup"),
		makeEvent("d"), makeEvent("dup"), makeEvent("dup"),
		makeEvent("dup"), makeEvent("dup"), makeEvent("dup"),
		makeEvent("e"), makeEvent("f"), makeEvent("dup"),
		makeEvent("dup"), makeEvent("dup"), makeEvent("g"),
		makeEvent("h"), makeEvent("dup"),
	}
	src := &channelSource{events: events, buf: 4}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{DedupeStage{Window: 100}},
		Sink:   sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	// 8 distinct titles + dup = 9 events out.
	if len(sink.got) != 9 {
		t.Fatalf("got %d events, want 9: %+v", len(sink.got), sink.got)
	}
	var dupCount int
	for _, ev := range sink.got {
		if ev.Title == "dup" {
			dupCount = ev.Count
		} else if ev.Count != 1 {
			t.Errorf("unique event %q Count=%d, want 1", ev.Title, ev.Count)
		}
	}
	if dupCount != 12 {
		t.Fatalf("dup Count=%d, want 12", dupCount)
	}
}

func TestDedupeStage_EvictionFlushesBeforeEOF(t *testing.T) {
	// With a small window, the LRU evicts eagerly. The earliest
	// events should reach the sink before the source closes.
	events := []event.Event{makeEvent("a"), makeEvent("b"), makeEvent("c"), makeEvent("d")}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{DedupeStage{Window: 2}},
		Sink:   sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(sink.got) != 4 {
		t.Fatalf("got %d events, want 4", len(sink.got))
	}
	wantOrder := []string{"a", "b", "c", "d"}
	for i, ev := range sink.got {
		if ev.Title != wantOrder[i] {
			t.Errorf("position %d Title=%q, want %q", i, ev.Title, wantOrder[i])
		}
	}
}

func TestDedupeStage_WindowZeroPassthrough(t *testing.T) {
	events := []event.Event{makeEvent("a"), makeEvent("a"), makeEvent("b")}
	src := &channelSource{events: events, buf: 1}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{DedupeStage{Window: 0}},
		Sink:   sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(sink.got) != 3 {
		t.Fatalf("got %d, want 3 (window=0 should not dedupe)", len(sink.got))
	}
	for _, ev := range sink.got {
		if ev.Count != 1 {
			t.Errorf("window=0 Count=%d, want 1", ev.Count)
		}
	}
}

// TestDedupeStage_ContextCancellation runs Pipeline.Run with a Source
// that never closes, cancels the context, and asserts that Run
// returns promptly and no goroutines leak. The exact error value is
// incidental — what matters is the stage drains and exits.
func TestDedupeStage_ContextCancellation(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	srcCh := make(chan event.Event)
	src := readerSource{ch: srcCh}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{DedupeStage{Window: 4}},
		Sink:   sink,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	cancel()
	select {
	case <-done:
		// Whatever Run returns, it returned in time. Good.
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

// readerSource feeds a pre-existing channel through Source.
type readerSource struct{ ch <-chan event.Event }

func (r readerSource) Source(_ context.Context) (<-chan event.Event, error) {
	return r.ch, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
