package output

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// sinkFactory builds a Sink that writes to w. NoFooter flips the
// footer-suppression flag (a no-op for JSONSink, see godoc).
type sinkFactory struct {
	name     string
	newSink  func(w *bytes.Buffer, noFooter bool, counters *pipeline.BudgetCounters) pipeline.Sink
	emittedN func(pipeline.Sink) int
}

func allSinks() []sinkFactory {
	return []sinkFactory{
		{
			name: "text",
			newSink: func(w *bytes.Buffer, nf bool, c *pipeline.BudgetCounters) pipeline.Sink {
				return &TextSink{Writer: w, NoFooter: nf, FormatName: "pytest", InputLines: 100, Counters: c}
			},
			emittedN: func(s pipeline.Sink) int { return s.(*TextSink).EventsEmitted() },
		},
		{
			name: "json-batch",
			newSink: func(w *bytes.Buffer, nf bool, c *pipeline.BudgetCounters) pipeline.Sink {
				return &JSONSink{Writer: w, NoFooter: nf, FormatName: "pytest", InputLines: 100, Counters: c}
			},
			emittedN: func(s pipeline.Sink) int { return s.(*JSONSink).EventsEmitted() },
		},
		{
			name: "json-stream",
			newSink: func(w *bytes.Buffer, nf bool, c *pipeline.BudgetCounters) pipeline.Sink {
				return &JSONSink{Writer: w, NoFooter: nf, FormatName: "pytest", InputLines: 100, Counters: c, Streaming: true}
			},
			emittedN: func(s pipeline.Sink) int { return s.(*JSONSink).EventsEmitted() },
		},
		{
			name: "markdown",
			newSink: func(w *bytes.Buffer, nf bool, c *pipeline.BudgetCounters) pipeline.Sink {
				return &MarkdownSink{Writer: w, NoFooter: nf, FormatName: "pytest", InputLines: 100, Counters: c}
			},
			emittedN: func(s pipeline.Sink) int { return s.(*MarkdownSink).EventsEmitted() },
		},
	}
}

// TestSinks_DeterministicForFixedInput exercises the determinism
// invariant per the project's testing rule: same []Event fed to each
// Sink twice must produce byte-identical output.
func TestSinks_DeterministicForFixedInput(t *testing.T) {
	evs := []event.Event{
		simpleEvent("error", "first"),
		simpleEvent("warn", "second"),
		simpleEvent("info", "third"),
	}
	evs[1].Count = 4
	evs[1].FramesCollapsed = 3
	evs[2].Metadata = map[string]string{"b": "2", "a": "1"}
	for _, f := range allSinks() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			var a, b bytes.Buffer
			sa := f.newSink(&a, false, nil)
			sb := f.newSink(&b, false, nil)
			feedSink(t, sa, evs)
			feedSink(t, sb, evs)
			if !bytes.Equal(a.Bytes(), b.Bytes()) {
				t.Errorf("non-deterministic output\n--- run1 ---\n%s\n--- run2 ---\n%s", a.String(), b.String())
			}
		})
	}
}

// TestSinks_StreamingEmitsBeforeEOF asserts that every streaming Sink
// emits the first event before the channel closes. JSON-batch is
// excluded by design — it buffers because the schema requires a single
// top-level object — and is tested separately.
func TestSinks_StreamingEmitsBeforeEOF(t *testing.T) {
	streamingFactories := []sinkFactory{}
	for _, f := range allSinks() {
		if f.name == "json-batch" {
			continue
		}
		streamingFactories = append(streamingFactories, f)
	}
	for _, f := range streamingFactories {
		f := f
		t.Run(f.name, func(t *testing.T) {
			w := newProbeWriter()
			// Use a thin probeWriter wrapper that satisfies *bytes.Buffer
			// indirectly: each Sink's Writer field is io.Writer, so the
			// concrete type is fine — but the factory uses *bytes.Buffer.
			// Workaround: rebuild the Sink directly here.
			var s pipeline.Sink
			switch f.name {
			case "text":
				s = &TextSink{Writer: w, FormatName: "pytest", NoFooter: true}
			case "json-stream":
				s = &JSONSink{Writer: w, FormatName: "pytest", Streaming: true}
			case "markdown":
				s = &MarkdownSink{Writer: w, FormatName: "pytest", NoFooter: true}
			default:
				t.Skipf("no streaming factory for %s", f.name)
			}
			ch := make(chan event.Event)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := s.Sink(ctx, ch); err != nil {
					t.Errorf("Sink: %v", err)
				}
			}()
			ch <- simpleEvent("error", "marker")
			if !waitForSubstring(w, "marker", 1000) {
				t.Fatalf("sink %s buffered the first event:\n%s", f.name, w.snapshot())
			}
			ch <- simpleEvent("error", "second")
			close(ch)
			wg.Wait()
		})
	}
}

// TestSinks_NoFooterFlagHonoured proves that the human-readable
// encoders honour NoFooter and that JSONSink's documented no-op
// behaviour holds.
func TestSinks_NoFooterFlagHonoured(t *testing.T) {
	evs := []event.Event{simpleEvent("error", "x")}
	for _, f := range allSinks() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			var with, without bytes.Buffer
			sa := f.newSink(&with, false, nil)
			sb := f.newSink(&without, true, nil)
			feedSink(t, sa, evs)
			feedSink(t, sb, evs)
			switch f.name {
			case "text":
				if !bytes.Contains(with.Bytes(), []byte("\n---\n")) {
					t.Fatalf("text default missing ---")
				}
				if bytes.Contains(without.Bytes(), []byte("\n---\n")) {
					t.Fatalf("text NoFooter still emits ---")
				}
			case "markdown":
				if !bytes.Contains(with.Bytes(), []byte("\n---\n\n")) {
					t.Fatalf("markdown default missing ---")
				}
				if bytes.Contains(without.Bytes(), []byte("\n---\n\n")) {
					t.Fatalf("markdown NoFooter still emits ---")
				}
			case "json-batch", "json-stream":
				if !bytes.Equal(with.Bytes(), without.Bytes()) {
					t.Fatalf("%s NoFooter should be a no-op; outputs differ", f.name)
				}
				if !bytes.Contains(with.Bytes(), []byte(`"summary"`)) {
					t.Fatalf("%s output missing summary block", f.name)
				}
			}
		})
	}
}

// TestSinks_FooterReflectsCounters synthesises a BudgetCounters value
// with known fields, runs each Sink, and asserts the relevant counter
// values appear in (or are reachable from) the output.
func TestSinks_FooterReflectsCounters(t *testing.T) {
	counters := &pipeline.BudgetCounters{
		EventsBuffered:      7,
		EventsEmitted:       3,
		EventsDroppedBudget: 4,
		EventsTruncated:     1,
		EstimatedTokens:     250,
	}
	evs := []event.Event{
		simpleEvent("error", "x"),
		simpleEvent("warn", "y"),
		simpleEvent("info", "z"),
	}
	evs[1].FramesCollapsed = 12
	evs[2].Count = 3
	for _, f := range allSinks() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := f.newSink(&buf, false, counters)
			feedSink(t, s, evs)
			out := buf.String()
			switch f.name {
			case "text":
				if !strings.Contains(out, "4 events") {
					t.Errorf("text footer missing dropped count")
				}
				if !strings.Contains(out, "250") {
					t.Errorf("text footer missing tokens")
				}
				if !strings.Contains(out, "12 vendor frames") {
					t.Errorf("text footer missing vendor frame count")
				}
				if !strings.Contains(out, "2 deduped") {
					t.Errorf("text footer missing deduped count (Count=3 → 2 deduped)")
				}
			case "markdown":
				if !strings.Contains(out, "**Events dropped:** 4") {
					t.Errorf("markdown footer missing dropped count")
				}
				if !strings.Contains(out, "250") {
					t.Errorf("markdown footer missing tokens")
				}
				if !strings.Contains(out, "**Vendor frames removed:** 12") {
					t.Errorf("markdown footer missing vendor frames")
				}
			case "json-batch", "json-stream":
				if !strings.Contains(out, `"events_dropped_budget": 4`) &&
					!strings.Contains(out, `"events_dropped_budget":4`) {
					t.Errorf("%s summary missing events_dropped_budget=4", f.name)
				}
				if !strings.Contains(out, `"estimated_tokens": 250`) &&
					!strings.Contains(out, `"estimated_tokens":250`) {
					t.Errorf("%s summary missing estimated_tokens=250", f.name)
				}
				if !strings.Contains(out, `"frames_collapsed": 12`) &&
					!strings.Contains(out, `"frames_collapsed":12`) {
					t.Errorf("%s summary missing frames_collapsed=12", f.name)
				}
			}
		})
	}
}
