package output

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
)

func TestTextSink_Goldens(t *testing.T) {
	for _, c := range loadCases(t, "text") {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var buf bytes.Buffer
			s := &TextSink{
				Writer:        &buf,
				NoFooter:      c.NoFooter,
				FormatName:    c.FormatName,
				InputLines:    c.InputLines,
				EstimatorName: c.EstimatorName,
			}
			feedSink(t, s, c.Events)
			goldenCompare(t, "text", "txt", c, buf.Bytes())
		})
	}
}

// feedSink runs the Sink synchronously by feeding events through a
// channel that closes after the last one. It mimics what pipeline.Run
// does without spinning up the full pipeline machinery.
func feedSink(t *testing.T, s pipeline.Sink, evs []event.Event) {
	t.Helper()
	ch := make(chan event.Event, len(evs)+1)
	for i := range evs {
		ch <- evs[i]
	}
	close(ch)
	if err := s.Sink(context.Background(), ch); err != nil {
		t.Fatalf("Sink: %v", err)
	}
}

func TestTextSink_NoFooterSuppressesFooter(t *testing.T) {
	ev := simpleEvent("error", "boom")
	withFooter := runText(t, []event.Event{ev}, false)
	withoutFooter := runText(t, []event.Event{ev}, true)
	if !bytes.Contains(withFooter, []byte("---\n")) {
		t.Fatalf("expected footer separator in default output\n%s", withFooter)
	}
	if bytes.Contains(withoutFooter, []byte("---\n")) {
		t.Fatalf("NoFooter=true should suppress separator\n%s", withoutFooter)
	}
	if !bytes.Contains(withoutFooter, []byte("ERROR boom")) {
		t.Fatalf("event body should still be present without footer")
	}
}

func TestTextSink_HandlesNilLocation(t *testing.T) {
	ev := simpleEvent("warn", "deprecation")
	ev.Location = nil
	out := runText(t, []event.Event{ev}, true)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "  at ") {
			t.Fatalf("nil Location should not render an 'at' line: %q\n%s", line, out)
		}
	}
}

func TestTextSink_EmptyInputPrintsNoEventsFound(t *testing.T) {
	out := runText(t, nil, false)
	if !bytes.Contains(out, []byte("no events found")) {
		t.Fatalf("expected 'no events found' marker; got\n%s", out)
	}
}

func TestTextSink_StreamingEmitsBeforeEOF(t *testing.T) {
	// Drip three events at 30ms intervals; assert that the first event
	// block is observable on the writer before the channel closes.
	w := newProbeWriter()
	s := &TextSink{Writer: w, FormatName: "pytest", InputLines: 10, NoFooter: true}
	ch := make(chan event.Event)
	var wg sync.WaitGroup
	wg.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		defer wg.Done()
		if err := s.Sink(ctx, ch); err != nil {
			t.Errorf("Sink: %v", err)
		}
	}()
	for i, ev := range []event.Event{simpleEvent("error", "first"), simpleEvent("error", "second"), simpleEvent("error", "third")} {
		select {
		case ch <- ev:
		case <-ctx.Done():
			t.Fatalf("ctx expired sending event %d", i)
		}
		time.Sleep(30 * time.Millisecond)
		if i == 0 {
			// First event should be visible before any subsequent event.
			if !w.hasReceived("first") {
				t.Fatalf("expected first event to be emitted streaming; writer holds %q", w.snapshot())
			}
		}
	}
	close(ch)
	wg.Wait()
}

func TestTextSink_ContextCancellation(t *testing.T) {
	w := &bytes.Buffer{}
	s := &TextSink{Writer: w, FormatName: "pytest"}
	ch := make(chan event.Event)
	ctx, cancel := context.WithCancel(context.Background())
	pre := runtime.NumGoroutine()
	errCh := make(chan error, 1)
	go func() { errCh <- s.Sink(ctx, ch) }()
	// Feed one event, then cancel.
	ch <- simpleEvent("error", "x")
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected ctx.Err on cancellation, got nil")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Sink did not return after ctx cancel")
	}
	time.Sleep(10 * time.Millisecond)
	if leaked := runtime.NumGoroutine() - pre; leaked > 0 {
		t.Logf("goroutine delta: +%d (may be runtime noise)", leaked)
	}
	_ = ch
}

func TestTextSink_NilWriterErrors(t *testing.T) {
	s := &TextSink{}
	ch := make(chan event.Event)
	close(ch)
	if err := s.Sink(context.Background(), ch); err == nil {
		t.Fatalf("expected error for nil Writer")
	}
}

func TestTextSink_CountsEventsEmitted(t *testing.T) {
	evs := []event.Event{
		simpleEvent("error", "a"),
		simpleEvent("warn", "b"),
		simpleEvent("info", "c"),
	}
	var buf bytes.Buffer
	s := &TextSink{Writer: &buf, FormatName: "pytest", NoFooter: true}
	feedSink(t, s, evs)
	if got, want := s.EventsEmitted(), 3; got != want {
		t.Fatalf("EventsEmitted=%d want %d", got, want)
	}
}

func TestTextSink_FooterReflectsCounters(t *testing.T) {
	counters := &pipeline.BudgetCounters{
		EventsBuffered:      5,
		EventsEmitted:       2,
		EventsDroppedBudget: 3,
		EventsTruncated:     1,
		EstimatedTokens:     127,
	}
	var buf bytes.Buffer
	s := &TextSink{
		Writer:     &buf,
		FormatName: "pytest",
		InputLines: 999,
		Counters:   counters,
	}
	feedSink(t, s, []event.Event{simpleEvent("error", "x")})
	out := buf.String()
	if !strings.Contains(out, "3 events") {
		t.Errorf("footer should show 3 dropped events: %q", out)
	}
	if !strings.Contains(out, "127") {
		t.Errorf("footer should show 127 tokens: %q", out)
	}
}

// runText runs TextSink over evs and returns the encoded output. With
// emptyToo=true it also tests that NoFooter behaves correctly.
func runText(t *testing.T, evs []event.Event, noFooter bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	s := &TextSink{
		Writer:     &buf,
		FormatName: "pytest",
		InputLines: 1000,
		NoFooter:   noFooter,
	}
	feedSink(t, s, evs)
	return buf.Bytes()
}

func simpleEvent(sev, title string) event.Event {
	col := 0
	return event.Event{
		Severity: event.Severity(sev),
		Kind:     "test_failure",
		Title:    title,
		Location: &event.Location{File: "f.py", Line: 1, Column: &col},
		Body:     []string{title},
		Count:    1,
	}
}

// probeWriter is a thread-safe in-memory writer that supports a
// "has this substring appeared yet?" query without locking the
// underlying buffer's API.
type probeWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newProbeWriter() *probeWriter { return &probeWriter{} }

func (p *probeWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.Write(b)
}

func (p *probeWriter) hasReceived(s string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return bytes.Contains(p.buf.Bytes(), []byte(s))
}

func (p *probeWriter) snapshot() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

// Ensure io.Discard is referenced so the linter doesn't complain when
// experimenting with alternate writers in this file.
var _ io.Writer = io.Discard
