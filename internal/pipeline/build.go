package pipeline

import (
	"fmt"

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
		Source:     src,
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
	return p, nil
}
