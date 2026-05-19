package output

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// TextSink is the default Sink: a compact, line-oriented encoder
// suitable for both terminals and LLM context windows. It implements
// pipeline.Sink.
//
// Each Event is written to Writer as a self-contained block as soon as
// it arrives. The header line ("N events from <format>") is deferred
// until either the first event arrives — when the count is initially
// "1" and is rewritten on completion is impossible because output is
// streaming, so the header instead reads "events from <format>"
// without a leading count when the Sink starts emitting and the final
// total appears in the footer. When no events arrive at all the Sink
// writes a single "no events found" line.
//
// The footer (suppressed by NoFooter) reports the line counts and the
// counter totals from Counters and InputLines. Counters may be nil for
// pipelines built without a BudgetStage; the Sink computes the
// best-effort summary from what it observed.
type TextSink struct {
	// Writer receives the encoded output. Required.
	Writer io.Writer

	// NoFooter suppresses the trailing summary block.
	NoFooter bool

	// FormatName is the name of the format that fed the pipeline. It
	// is rendered in the header and footer; defaults to "input" if
	// empty so the output is always grammatical.
	FormatName string

	// Counters, if non-nil, carries BudgetStage's running totals. The
	// Sink reads it once after the input channel closes to populate
	// the footer.
	Counters *pipeline.BudgetCounters

	// InputLines is the number of lines the Source consumed. Set
	// before Run when the caller knows the count ahead of time
	// (tests). At runtime the CLI prefers LineSource (below): the
	// LineCounter is installed at pipeline construction but its
	// count is only final after the Source drains. When both are
	// set, LineSource wins; when neither is set, the footer renders
	// "?".
	InputLines int

	// LineSource, when non-nil, is queried at footer-write time so
	// the input-line count reflects every byte the Source actually
	// consumed. The CLI passes the same *LineCounter it wrapped the
	// input with. Tests can pass FixedLineSource(N) for a constant.
	LineSource LineSource

	// EstimatorName names the token estimator the pipeline used. The
	// text footer renders it for transparency. Empty defaults to
	// "heuristic" so the footer line is grammatical.
	EstimatorName string

	// emitted tracks how many events the Sink wrote, so the CLI (M8)
	// can decide exit code 1 ("no events"). Read via EventsEmitted
	// after Run returns.
	emitted int
}

// Sink implements pipeline.Sink. It reads from in until the channel
// closes, writing each event as it arrives. ctx cancellation aborts
// the loop and returns ctx.Err(); IO errors return immediately.
func (s *TextSink) Sink(ctx context.Context, in <-chan event.Event) error {
	if s.Writer == nil {
		return fmt.Errorf("output: TextSink.Writer is nil")
	}
	wc := &writeCounter{w: s.Writer}
	bw := bufio.NewWriter(wc)
	header := false
	firstEvent := true
	deduped := 0
	frames := 0
	estTokens := 0
	formatLabel := s.FormatName
	if formatLabel == "" {
		formatLabel = "input"
	}
	estName := s.EstimatorName
	if estName == "" {
		estName = "heuristic"
	}
	for {
		select {
		case <-ctx.Done():
			_ = bw.Flush()
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				if !header {
					if _, err := bw.WriteString("no events found\n"); err != nil {
						return err
					}
				}
				if !s.NoFooter {
					if err := writeTextFooter(bw, textFooter{
						formatName:    formatLabel,
						inputLines:    s.resolveInputLines(),
						outputLines:   wc.lines,
						eventsEmitted: s.emitted,
						eventsDeduped: deduped,
						framesRemoved: frames,
						estimator:     estName,
						estTokens:     estTokens,
						counters:      s.Counters,
					}); err != nil {
						return err
					}
				}
				return bw.Flush()
			}
			if firstEvent {
				firstEvent = false
				header = true
				if _, err := fmt.Fprintf(bw, "events from %s\n\n", formatLabel); err != nil {
					return err
				}
			}
			s.emitted++
			if ev.Count > 1 {
				deduped += ev.Count - 1
			}
			frames += ev.FramesCollapsed
			if err := writeTextEvent(bw, s.emitted, ev); err != nil {
				return err
			}
			if err := bw.Flush(); err != nil {
				return err
			}
		}
	}
}

// EventsEmitted reports how many events the Sink wrote. Called by the
// CLI (M8) after Pipeline.Run returns to decide exit code 1 ("no
// events").
func (s *TextSink) EventsEmitted() int { return s.emitted }

// resolveInputLines returns the input-line count to render in the
// footer. LineSource wins when set so the runtime LineCounter count
// is honoured; otherwise the static InputLines field is used.
func (s *TextSink) resolveInputLines() int {
	if s.LineSource != nil {
		return s.LineSource.Lines()
	}
	return s.InputLines
}

// writeTextEvent renders one Event block per the M7.1 spec.
func writeTextEvent(w io.Writer, idx int, ev event.Event) error {
	if _, err := fmt.Fprintf(w, "[%d] %s %s\n", idx, severityLabel(ev.Severity), ev.Title); err != nil {
		return err
	}
	if ev.Location != nil {
		if _, err := fmt.Fprintf(w, "  at %s:%d\n", ev.Location.File, ev.Location.Line); err != nil {
			return err
		}
	}
	for _, line := range ev.Body {
		if _, err := fmt.Fprintf(w, "  %s\n", line); err != nil {
			return err
		}
	}
	if len(ev.Context) > 0 {
		if _, err := w.Write([]byte("  context:\n")); err != nil {
			return err
		}
		for _, line := range ev.Context {
			if _, err := fmt.Fprintf(w, "    %s\n", line); err != nil {
				return err
			}
		}
	}
	if ev.FramesCollapsed > 0 {
		if _, err := fmt.Fprintf(w, "  ... %d vendor frames collapsed\n", ev.FramesCollapsed); err != nil {
			return err
		}
	}
	if ev.Count > 1 {
		if _, err := fmt.Fprintf(w, "  (×%d)\n", ev.Count); err != nil {
			return err
		}
	}
	if ev.Truncated {
		if _, err := w.Write([]byte("  [truncated by --budget]\n")); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

type textFooter struct {
	formatName    string
	inputLines    int
	outputLines   int
	eventsEmitted int
	eventsDeduped int
	framesRemoved int
	estimator     string
	estTokens     int
	counters      *pipeline.BudgetCounters
}

func writeTextFooter(w io.Writer, f textFooter) error {
	if _, err := w.Write([]byte("---\n")); err != nil {
		return err
	}
	inLines := "?"
	if f.inputLines > 0 {
		inLines = fmtCount(f.inputLines)
	}
	tokens := f.estTokens
	if f.counters != nil && f.counters.EstimatedTokens > tokens {
		tokens = f.counters.EstimatedTokens
	}
	if _, err := fmt.Fprintf(w, "distilled %s lines → %s lines (%s tokens, %s)\n",
		inLines, fmtCount(f.outputLines), fmtCount(tokens), f.estimator); err != nil {
		return err
	}
	dropped := 0
	truncated := 0
	if f.counters != nil {
		dropped = f.counters.EventsDroppedBudget
		truncated = f.counters.EventsTruncated
	}
	parts := []string{
		fmt.Sprintf("%s events", fmtCount(dropped)),
		fmt.Sprintf("%s truncated", fmtCount(truncated)),
		fmt.Sprintf("%s deduped", fmtCount(f.eventsDeduped)),
		fmt.Sprintf("%s vendor frames", fmtCount(f.framesRemoved)),
	}
	if _, err := fmt.Fprintf(w, "dropped: %s\n", strings.Join(parts, ", ")); err != nil {
		return err
	}
	return nil
}
