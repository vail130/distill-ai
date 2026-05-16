package pipeline

// Options bundles the per-run tunables the pipeline accepts. Zero
// values are safe defaults: DedupeWindow=0 disables dedupe (every
// Event passes through with Count=1), KeepVendor=false collapses
// vendor frames into a frames_collapsed count, and BufferSize=0 maps
// to DefaultBufferSize.
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

	// BufferSize sizes the inter-stage channels. Zero maps to
	// DefaultBufferSize.
	BufferSize int
}

// Build returns a Pipeline wired with the standard stage chain:
// CollapseStage first, DedupeStage second. The order matters —
// dedupe signatures key on the post-collapse frame layout, so any
// Event whose Title is derived from a frame (e.g., a parser that
// labels an event by its top user frame) will dedupe correctly.
// Build is the supported constructor; field-level Pipeline
// construction is reserved for tests that substitute custom stages.
func Build(src Source, sink Sink, opts Options) *Pipeline {
	return &Pipeline{
		Source: src,
		Stages: []Stage{
			// Collapse must run before Dedupe; see godoc.
			CollapseStage{KeepVendor: opts.KeepVendor},
			DedupeStage{Window: opts.DedupeWindow},
		},
		Sink:       sink,
		BufferSize: opts.BufferSize,
	}
}
