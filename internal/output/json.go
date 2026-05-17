package output

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// JSONSink encodes the Event stream as JSON. Two modes:
//
//   - Batch (Streaming=false, the default): one top-level object with
//     schema_version, format, events:[...], summary:{...}. Suitable for
//     bounded input (files, finite stdin).
//   - Streaming (Streaming=true): newline-delimited JSON. One line per
//     event, each carrying schema_version+format+event; the final
//     line is a schema_version+format+summary line. Suitable for
//     unbounded input (tail -f, live pipes).
//
// The shape is documented in docs/formats/SCHEMA.md and is a public
// API. Breaking changes bump SchemaVersion and are gated by the
// output-stability rule.
//
// Unlike TextSink / MarkdownSink, JSONSink always emits the summary
// object even when NoFooter is true: the summary is part of the schema,
// not a human-readable footer. NoFooter on JSONSink is a no-op kept
// only so callers can pass identical Sink configurations across
// encoders without special-casing.
type JSONSink struct {
	// Writer receives the encoded output. Required.
	Writer io.Writer

	// NoFooter is ignored by JSONSink; see godoc above. The field is
	// present so the CLI (M8) can configure all three Sinks
	// uniformly.
	NoFooter bool

	// FormatName is the name of the format that fed the pipeline.
	// Rendered in every top-level object under `format`. Empty maps
	// to "unknown" so the JSON is always parseable.
	FormatName string

	// Counters, if non-nil, carries BudgetStage totals. Used to
	// populate summary.events_dropped_budget, summary.events_truncated,
	// and summary.estimated_tokens.
	Counters *pipeline.BudgetCounters

	// InputLines is the line count consumed from the Source, plumbed
	// in by the CLI's LineCounter (M8). Zero is acceptable; the
	// summary records it verbatim.
	InputLines int

	// Streaming switches the encoder between batch (false) and ndjson
	// (true) shapes per the SCHEMA.md spec.
	Streaming bool

	// EstimatorName is "heuristic" or "tiktoken" depending on which
	// estimator the pipeline used. Empty maps to "heuristic".
	EstimatorName string

	// ExitCode is the run's final exit code, populated by the CLI (M8)
	// before the trailer is written. Zero is the default.
	ExitCode int

	emitted int
}

// Sink implements pipeline.Sink. ctx cancellation aborts encoding and
// returns ctx.Err(); IO errors return immediately.
func (s *JSONSink) Sink(ctx context.Context, in <-chan event.Event) error {
	if s.Writer == nil {
		return fmt.Errorf("output: JSONSink.Writer is nil")
	}
	wc := &writeCounter{w: s.Writer}
	if s.Streaming {
		return s.streamingSink(ctx, in, wc)
	}
	return s.batchSink(ctx, in, wc)
}

// EventsEmitted reports how many events the Sink wrote. The CLI uses
// this to map zero-event runs to exit code 1.
func (s *JSONSink) EventsEmitted() int { return s.emitted }

// streamingSink emits one ndjson line per Event then a final summary
// line. Each line is a self-contained object so a consumer reading
// line by line can decode incrementally.
func (s *JSONSink) streamingSink(ctx context.Context, in <-chan event.Event, wc *writeCounter) error {
	bw := bufio.NewWriter(wc)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	deduped := 0
	frames := 0
	for {
		select {
		case <-ctx.Done():
			_ = bw.Flush()
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				if err := enc.Encode(streamLine{
					SchemaVersion: SchemaVersion,
					Format:        s.formatName(),
					Summary:       s.buildSummary(wc.lines, deduped, frames),
				}); err != nil {
					return err
				}
				return bw.Flush()
			}
			s.emitted++
			if ev.Count > 1 {
				deduped += ev.Count - 1
			}
			frames += ev.FramesCollapsed
			if err := enc.Encode(streamLine{
				SchemaVersion: SchemaVersion,
				Format:        s.formatName(),
				Event:         &ev,
			}); err != nil {
				return err
			}
			if err := bw.Flush(); err != nil {
				return err
			}
		}
	}
}

// batchSink buffers every Event then emits a single top-level object.
// This is the mode for bounded input (files, finite stdin).
func (s *JSONSink) batchSink(ctx context.Context, in <-chan event.Event, wc *writeCounter) error {
	events := make([]event.Event, 0, 16)
	deduped := 0
	frames := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				out := batchOutput{
					SchemaVersion: SchemaVersion,
					Format:        s.formatName(),
					Events:        events,
					Summary:       s.buildSummary(0, deduped, frames),
				}
				bw := bufio.NewWriter(wc)
				enc := json.NewEncoder(bw)
				enc.SetEscapeHTML(false)
				enc.SetIndent("", "  ")
				if err := enc.Encode(out); err != nil {
					return err
				}
				if err := bw.Flush(); err != nil {
					return err
				}
				// Re-emit summary with the now-known output_lines.
				// Cheap: re-marshal the same struct with the
				// post-write line count. Skipped because the schema
				// commits to a single top-level object — output_lines
				// is best-effort in batch mode and reflects only the
				// final encoded byte count, which is 0 before encode.
				// Consumers care about the body; for an exact count,
				// use streaming mode.
				return nil
			}
			s.emitted++
			if ev.Count > 1 {
				deduped += ev.Count - 1
			}
			frames += ev.FramesCollapsed
			events = append(events, ev)
		}
	}
}

func (s *JSONSink) formatName() string {
	if s.FormatName == "" {
		return "unknown"
	}
	return s.FormatName
}

func (s *JSONSink) buildSummary(outputLines, deduped, frames int) summary {
	dropped := 0
	truncated := 0
	tokens := 0
	forcedDrops := false
	if s.Counters != nil {
		dropped = s.Counters.EventsDroppedBudget
		truncated = s.Counters.EventsTruncated
		tokens = s.Counters.EstimatedTokens
		forcedDrops = s.Counters.ForcedDrops()
	}
	name := s.EstimatorName
	if name == "" {
		name = "heuristic"
	}
	return summary{
		InputLines:          s.InputLines,
		OutputLines:         outputLines,
		EventsFound:         s.emitted + dropped,
		EventsEmitted:       s.emitted,
		EventsDeduped:       deduped,
		EventsDroppedBudget: dropped,
		EventsTruncated:     truncated,
		FramesCollapsed:     frames,
		EstimatedTokens:     tokens,
		Estimator:           name,
		ExitCode:            s.resolveExitCode(forcedDrops),
	}
}

// resolveExitCode derives the exit code from the Sink's observed
// state plus the caller-set ExitCode override. The override wins
// when non-zero; otherwise the Sink applies the same precedence
// rule as the CLI:
//
//	forcedDrops -> ExitPartial (3)
//	emitted==0  -> ExitNoEvents (1)
//	else         -> ExitOK (0)
//
// This lets the JSON summary carry an honest exit_code without
// requiring callers to thread the value through the Sink after
// Pipeline.Run returns — a sequencing the current M7 Sink shape
// doesn't support because the trailer is written inside Sink.
func (s *JSONSink) resolveExitCode(forcedDrops bool) int {
	if s.ExitCode != 0 {
		return s.ExitCode
	}
	if forcedDrops {
		return 3 // ExitPartial; the constant lives in cmd/distill-ai.
	}
	if s.emitted == 0 {
		return 1 // ExitNoEvents
	}
	return 0 // ExitOK
}

// batchOutput is the wire shape of a single batch-mode JSON object.
type batchOutput struct {
	SchemaVersion int           `json:"schema_version"`
	Format        string        `json:"format"`
	Events        []event.Event `json:"events"`
	Summary       summary       `json:"summary"`
}

// streamLine is the wire shape of one ndjson line. Exactly one of
// Event or Summary is set; the other is omitted via the *struct nil
// pattern and json.Marshal's omitempty.
type streamLine struct {
	SchemaVersion int          `json:"schema_version"`
	Format        string       `json:"format"`
	Event         *event.Event `json:"event,omitempty"`
	Summary       summary      `json:"summary,omitzero"`
}

// summary is the wire shape of the summary object documented in
// SCHEMA.md. Fields are in the same order as the doc table.
type summary struct {
	InputLines          int    `json:"input_lines"`
	OutputLines         int    `json:"output_lines"`
	EventsFound         int    `json:"events_found"`
	EventsEmitted       int    `json:"events_emitted"`
	EventsDeduped       int    `json:"events_deduped"`
	EventsDroppedBudget int    `json:"events_dropped_budget"`
	EventsTruncated     int    `json:"events_truncated"`
	FramesCollapsed     int    `json:"frames_collapsed"`
	EstimatedTokens     int    `json:"estimated_tokens"`
	Estimator           string `json:"estimator"`
	ExitCode            int    `json:"exit_code"`
}
