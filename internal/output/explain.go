package output

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// ExplainSink emits a dry-run annotation of each event the pipeline
// produces. It is the Sink for `distill-ai explain FILE`.
//
// Per-emitted event, the Sink writes one line:
//
//	kept   <severity> <title> [at file:line] [<dedupe-evicted=K-1>] [<vendor-collapsed=N>] [<truncated>]
//
// The bracketed fragments are only included when relevant. The
// "dedupe-evicted" count is derived from Event.Count (a Count=K
// event represents K-1 dedupe-evicted drops collapsed into the
// surviving event); "vendor-collapsed" reflects
// Event.FramesCollapsed; "truncated" reflects Event.Truncated.
//
// After the input channel closes, the Sink reads the ExplainLog
// and emits one line per recorded drop:
//
//	dropped:<reason> <severity> <title> [at file:line]
//
// Reasons emitted today by the ExplainingBudgetStage:
//
//   - "budget" — the event was dropped or truncated to fit --budget.
//
// "severity-filter" entries are added by format parsers (M9.4 onward)
// when the parser filters before emitting. "dedupe-evicted" and
// "vendor-collapsed" are derived from emitted event fields, not from
// the log, so they appear inline on the kept-event line.
//
// Like the other Sinks, ExplainSink reads ctx and aborts on
// cancellation.
type ExplainSink struct {
	// Writer receives the diagnostic output. Required.
	Writer io.Writer

	// Log is the explain log populated by ExplainingBudgetStage and
	// any future drop-recording stages. Optional; nil means the
	// Sink only emits kept lines (no dropped section).
	Log *pipeline.ExplainLog

	emitted int
}

// Sink implements pipeline.Sink.
func (s *ExplainSink) Sink(ctx context.Context, in <-chan event.Event) error {
	if s.Writer == nil {
		return fmt.Errorf("output: ExplainSink.Writer is nil")
	}
	bw := bufio.NewWriter(s.Writer)
	defer func() { _ = bw.Flush() }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return s.writeDropped(bw)
			}
			s.emitted++
			if err := writeKept(bw, ev); err != nil {
				return err
			}
		}
	}
}

// EventsEmitted reports how many "kept" lines the Sink wrote. The
// CLI uses this to compute exit code 1 ("no events") — but note
// that for ExplainSink "no events" means "the pipeline kept
// nothing", which is meaningful (it tells the user the budget or
// filters dropped everything).
func (s *ExplainSink) EventsEmitted() int { return s.emitted }

// writeDropped emits the dropped-events block. Called after the
// input channel closes.
func (s *ExplainSink) writeDropped(w *bufio.Writer) error {
	if s.Log == nil || s.Log.Len() == 0 {
		return nil
	}
	for _, e := range s.Log.Entries() {
		if err := writeDroppedLine(w, e); err != nil {
			return err
		}
	}
	return nil
}

// writeKept renders one "kept ..." line.
func writeKept(w *bufio.Writer, ev event.Event) error {
	if _, err := fmt.Fprintf(w, "kept   %s %s", severityLabelExplain(ev.Severity), ev.Title); err != nil {
		return err
	}
	if ev.Location != nil {
		if _, err := fmt.Fprintf(w, " at %s:%d", ev.Location.File, ev.Location.Line); err != nil {
			return err
		}
	}
	if ev.Count > 1 {
		if _, err := fmt.Fprintf(w, " <dedupe-evicted=%d>", ev.Count-1); err != nil {
			return err
		}
	}
	if ev.FramesCollapsed > 0 {
		if _, err := fmt.Fprintf(w, " <vendor-collapsed=%d>", ev.FramesCollapsed); err != nil {
			return err
		}
	}
	if ev.Truncated {
		if _, err := fmt.Fprint(w, " <truncated>"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// writeDroppedLine renders one "dropped:<reason> ..." line.
func writeDroppedLine(w *bufio.Writer, e pipeline.ExplainEntry) error {
	if _, err := fmt.Fprintf(w, "dropped:%s %s %s", e.Reason, severityLabelExplain(e.Severity), e.Title); err != nil {
		return err
	}
	if e.Location != nil {
		if _, err := fmt.Fprintf(w, " at %s:%d", e.Location.File, e.Location.Line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// severityLabelExplain renders a severity for explain output.
// Mirrors the project's severityLabel but stays local so a future
// change to the human-readable text encoder's labels doesn't
// accidentally re-shape explain output.
func severityLabelExplain(s event.Severity) string {
	switch s {
	case event.SeverityError:
		return "ERROR"
	case event.SeverityWarn:
		return "WARN"
	case event.SeverityInfo:
		return "INFO"
	default:
		return string(s)
	}
}
