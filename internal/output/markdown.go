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

// MarkdownSink encodes the Event stream as Markdown suitable for direct
// paste into a chat with an LLM or a colleague. Same content as TextSink
// — same blocks, same footer — but with `###` headings, bullet metadata
// lists, and fenced code blocks around bodies and context.
//
// FenceLang sets the code-block language. Empty (the default) emits
// language-less fences; CLI callers can pass "python", "go", etc. to
// get syntax-highlighted blocks in the rendered Markdown.
type MarkdownSink struct {
	// Writer receives the encoded output. Required.
	Writer io.Writer

	// NoFooter suppresses the trailing `---` summary block.
	NoFooter bool

	// FormatName is the format that fed the pipeline; rendered in the
	// header and footer. Empty maps to "input".
	FormatName string

	// FenceLang is the language hint placed after the opening
	// triple-backtick fence on body and context blocks. Empty
	// produces language-less fences.
	FenceLang string

	// Counters, if non-nil, carries BudgetStage totals for the footer.
	Counters *pipeline.BudgetCounters

	// InputLines is the line count the Source consumed.
	InputLines int

	// EstimatorName is the token estimator the pipeline used.
	EstimatorName string

	emitted int
}

// Sink implements pipeline.Sink.
func (s *MarkdownSink) Sink(ctx context.Context, in <-chan event.Event) error {
	if s.Writer == nil {
		return fmt.Errorf("output: MarkdownSink.Writer is nil")
	}
	wc := &writeCounter{w: s.Writer}
	bw := bufio.NewWriter(wc)
	header := false
	firstEvent := true
	deduped := 0
	frames := 0
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
					if _, err := bw.WriteString("_no events found_\n"); err != nil {
						return err
					}
				}
				if !s.NoFooter {
					if err := writeMarkdownFooter(bw, textFooter{
						formatName:    formatLabel,
						inputLines:    s.InputLines,
						outputLines:   wc.lines,
						eventsEmitted: s.emitted,
						eventsDeduped: deduped,
						framesRemoved: frames,
						estimator:     estName,
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
				if _, err := fmt.Fprintf(bw, "# events from %s\n\n", formatLabel); err != nil {
					return err
				}
			}
			s.emitted++
			if ev.Count > 1 {
				deduped += ev.Count - 1
			}
			frames += ev.FramesCollapsed
			if err := writeMarkdownEvent(bw, s.emitted, ev, s.FenceLang); err != nil {
				return err
			}
			if err := bw.Flush(); err != nil {
				return err
			}
		}
	}
}

// EventsEmitted reports how many events the Sink wrote.
func (s *MarkdownSink) EventsEmitted() int { return s.emitted }

func writeMarkdownEvent(w io.Writer, idx int, ev event.Event, fence string) error {
	if _, err := fmt.Fprintf(w, "### [%d] %s %s\n\n", idx, severityLabel(ev.Severity), ev.Title); err != nil {
		return err
	}
	// Bullet list of metadata. Each bullet is optional but appears in
	// a fixed order so the rendered Markdown is consistent across
	// events.
	bullets := make([]string, 0, 4)
	if ev.Location != nil {
		bullets = append(bullets, fmt.Sprintf("**Location:** `%s:%d`", ev.Location.File, ev.Location.Line))
	}
	if ev.Count > 1 {
		bullets = append(bullets, fmt.Sprintf("**Count:** ×%d", ev.Count))
	}
	if ev.FramesCollapsed > 0 {
		bullets = append(bullets, fmt.Sprintf("**Vendor frames collapsed:** %d", ev.FramesCollapsed))
	}
	if ev.Truncated {
		bullets = append(bullets, "**Truncated by --budget**")
	}
	for _, b := range bullets {
		if _, err := fmt.Fprintf(w, "- %s\n", b); err != nil {
			return err
		}
	}
	if len(bullets) > 0 {
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
	}
	if len(ev.Body) > 0 {
		if err := writeFenced(w, fence, ev.Body); err != nil {
			return err
		}
	}
	if len(ev.Context) > 0 {
		if _, err := w.Write([]byte("**Context:**\n\n")); err != nil {
			return err
		}
		if err := writeFenced(w, fence, ev.Context); err != nil {
			return err
		}
	}
	return nil
}

func writeFenced(w io.Writer, lang string, lines []string) error {
	if _, err := fmt.Fprintf(w, "```%s\n", lang); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte("```\n\n")); err != nil {
		return err
	}
	return nil
}

func writeMarkdownFooter(w io.Writer, f textFooter) error {
	if _, err := w.Write([]byte("---\n\n")); err != nil {
		return err
	}
	inLines := "?"
	if f.inputLines > 0 {
		inLines = fmtCount(f.inputLines)
	}
	tokens := 0
	if f.counters != nil {
		tokens = f.counters.EstimatedTokens
	}
	dropped := 0
	if f.counters != nil {
		dropped = f.counters.EventsDroppedBudget
	}
	lines := []string{
		fmt.Sprintf("- **Lines distilled:** %s → %s", inLines, fmtCount(f.outputLines)),
		fmt.Sprintf("- **Events emitted:** %s", fmtCount(f.eventsEmitted)),
		fmt.Sprintf("- **Events dropped:** %s", fmtCount(dropped)),
		fmt.Sprintf("- **Events deduped:** %s", fmtCount(f.eventsDeduped)),
		fmt.Sprintf("- **Vendor frames removed:** %s", fmtCount(f.framesRemoved)),
		fmt.Sprintf("- **Estimated tokens:** %s (%s)", fmtCount(tokens), f.estimator),
	}
	if _, err := w.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		return err
	}
	return nil
}
