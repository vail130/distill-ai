package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/tokens"
)

// Options bundles the per-run tunables the pipeline accepts. Zero
// values are safe defaults: DedupeWindow=0 disables dedupe (every
// Event passes through with Count=1), KeepVendor=false collapses
// vendor frames into a frames_collapsed count, Budget=0 disables
// budget enforcement (every Event flows through unchanged), and
// BufferSize=0 maps to DefaultBufferSize.
//
// Options is exposed so the CLI (M8) and library callers (M14) can
// pass a single value through to Build instead of populating
// Pipeline field-by-field. Pipeline's exported fields remain valid
// for tests that need to substitute custom Stages.
type Options struct {
	// DedupeWindow is the LRU capacity used by DedupeStage. Zero or
	// negative disables dedupe. CLI flag: --dedupe-window=N (M8).
	DedupeWindow int

	// KeepVendor leaves vendor frames in place (re-classified for
	// encoder styling) when true. CLI flag: --keep-vendor (M8).
	KeepVendor bool

	// Budget caps the estimated total token cost of the emitted
	// Event stream. Zero (the default) disables budget enforcement.
	// When non-zero, BudgetStage is appended to the chain and the
	// returned Pipeline carries a populated BudgetCounters. CLI
	// flag: --budget=N (M8).
	Budget int

	// Tokenizer names the token estimator BudgetStage uses. Valid
	// values: "heuristic" (default), "tiktoken". An unknown value
	// causes Build to return an error. CLI flag: --tokenizer (M8).
	Tokenizer string

	// BufferSize sizes the inter-stage channels. Zero maps to
	// DefaultBufferSize.
	BufferSize int

	// MaxEvents caps the number of Events that may pass through to
	// the Sink. Zero (the default) disables the cap. When set,
	// Build appends a MaxEventsStage at the end of the chain so
	// the budget enforcer's truncation / drop decisions run
	// against the full stream before the cap is applied. CLI flag:
	// --max-events=N (M14.6).
	MaxEvents int

	// EnvelopeSignals, when non-nil, is a channel of envelope-level
	// signal Events (envelope_error, envelope_warning,
	// envelope_step_failure) produced by an envelope.Stripper. Build
	// wraps the supplied Source in a fan-in Source that merges
	// signals with the format parser's Event stream before either
	// reaches Stages. The fan-in preserves arrival order across the
	// two channels (whichever has data ready next wins, per Go
	// select semantics).
	//
	// The channel is consumed exactly once by the wrapper; the
	// caller may not read from it after passing it here. When
	// either channel closes the wrapper continues draining the
	// other until both are done, then closes its own output. CLI
	// wiring lives in cmd/distill-ai/run.go (M13.2).
	EnvelopeSignals <-chan event.Event
}

// Build returns a Pipeline wired with the standard stage chain. The
// base chain is CollapseStage → DedupeStage, in that order, because
// dedupe signatures key on the post-collapse frame layout so any
// Event whose Title is derived from a frame (e.g., a parser that
// labels an event by its top user frame) dedupes correctly. When
// Options.Budget > 0, BudgetStage is appended to the end of the
// chain and the returned Pipeline's BudgetCounters field is
// populated so the Sink (M7) and library callers (M14) can read
// budget statistics after Run returns.
//
// When Options.EnvelopeSignals is non-nil, Build wraps src in a
// fan-in Source that merges the envelope-level signal stream with
// the parser's Event stream upstream of any Stage. The signals are
// indistinguishable from parser Events to the rest of the pipeline:
// they participate in Collapse, Dedupe, and Budget exactly the same
// way and reach the Sink with their envelope_* Kinds intact.
//
// Build is the supported constructor; field-level Pipeline
// construction is reserved for tests that substitute custom Stages.
// Build returns an error when Options.Budget > 0 and the requested
// Tokenizer cannot be resolved, so no goroutine starts on a
// misconfigured run.
func Build(src Source, sink Sink, opts Options) (*Pipeline, error) {
	stages := []Stage{
		// Collapse must run before Dedupe; see godoc.
		CollapseStage{KeepVendor: opts.KeepVendor},
		DedupeStage{Window: opts.DedupeWindow},
	}
	p := &Pipeline{
		Source:     wrapWithEnvelopeSignals(src, opts.EnvelopeSignals),
		Stages:     stages,
		Sink:       sink,
		BufferSize: opts.BufferSize,
	}
	if opts.Budget > 0 {
		est, err := tokens.ByName(opts.Tokenizer)
		if err != nil {
			return nil, fmt.Errorf("pipeline: build: %w", err)
		}
		counters := &BudgetCounters{}
		p.BudgetCounters = counters
		p.Stages = append(p.Stages, BudgetStage{
			Budget:    opts.Budget,
			Estimator: est,
			Counters:  counters,
		})
	}
	if opts.MaxEvents > 0 {
		// MaxEvents runs after BudgetStage so the budget's
		// severity-priority truncation operates on the full
		// stream; the cap then trims the top N highest-severity
		// Events.
		p.Stages = append(p.Stages, MaxEventsStage{Limit: opts.MaxEvents})
	}
	return p, nil
}

// wrapWithEnvelopeSignals returns src unchanged when signals is nil;
// otherwise it returns a Source that merges signals into src's Event
// channel. The wrapper preserves src's Source() error path.
func wrapWithEnvelopeSignals(src Source, signals <-chan event.Event) Source {
	if signals == nil {
		return src
	}
	return &mergedSource{primary: src, signals: signals}
}

// mergedSource fans a primary Source's Event channel together with
// an envelope.Stripper's signals channel into one stream. The
// merging goroutine reads from whichever channel has data ready and
// forwards the Event downstream; arrival order across the two
// inputs is preserved by Go's select semantics.
//
// Lifecycle:
//
//   - Source(ctx) starts both the primary Source and the merger
//     goroutine. If the primary Source returns an error, mergedSource
//     propagates it unchanged so callers see the same error path as
//     a bare Source.
//   - The merger drains both channels until both close, then closes
//     its output channel. A cancelled ctx returns promptly and both
//     channels are drained-and-discarded so no goroutine leaks.
type mergedSource struct {
	primary Source
	signals <-chan event.Event
}

// Source implements Source. The error return path mirrors the
// primary Source's: if it errors before producing a channel, we
// surface that error and the signals channel is not consumed.
func (m *mergedSource) Source(ctx context.Context) (<-chan event.Event, error) {
	if m.primary == nil {
		return nil, errors.New("mergedSource: primary Source is nil")
	}
	primaryCh, err := m.primary.Source(ctx)
	if err != nil {
		return nil, err
	}
	out := make(chan event.Event, DefaultBufferSize)
	go func() {
		defer close(out)
		primary := primaryCh
		signals := m.signals
		for primary != nil || signals != nil {
			select {
			case <-ctx.Done():
				// Drain both channels in the background so the
				// upstream producers don't block; we never read
				// the values. They exit once their producers
				// notice ctx.
				if primary != nil {
					go drain(primary)
				}
				if signals != nil {
					go drain(signals)
				}
				return
			case ev, ok := <-primary:
				if !ok {
					primary = nil
					continue
				}
				if !forward(ctx, out, ev) {
					return
				}
			case ev, ok := <-signals:
				if !ok {
					signals = nil
					continue
				}
				if !forward(ctx, out, ev) {
					return
				}
			}
		}
	}()
	return out, nil
}

// forward sends ev on out, honouring ctx cancellation. Returns false
// if ctx fired before the send completed; the caller should exit.
func forward(ctx context.Context, out chan<- event.Event, ev event.Event) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- ev:
		return true
	}
}

// drain reads from ch until close, discarding values. Used by
// mergedSource to keep its inputs from blocking after a ctx cancel.
func drain(ch <-chan event.Event) {
	for range ch { //nolint:revive // empty block is intentional: discard until close.
	}
}
