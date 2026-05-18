package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/output"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// newExplainCmd returns the cobra command for `distill-ai explain
// [FORMAT] [FILE...]`. It runs the same pipeline the run subcommand
// uses but with an instrumented BudgetStage and an ExplainSink, so
// the output is a per-event diagnostic of what was kept vs dropped
// and why.
//
// The output is line-oriented:
//
//	kept   <SEVERITY> <title> [at file:line] [<dedupe-evicted=K>] [<vendor-collapsed=N>] [<truncated>]
//	dropped:<reason> <SEVERITY> <title> [at file:line]
//
// Drop reasons today:
//
//   - "budget"          — BudgetStage dropped or truncated this event.
//   - "severity-filter" — the format's parser filtered the event (M9.4+).
//
// "dedupe-evicted" and "vendor-collapsed" are not "dropped" — the
// events emerged collapsed into a surviving event, and the explain
// sink surfaces the counts inline on the kept line.
//
// Exit codes follow the same rules as run (ExitOK / ExitNoEvents /
// ExitError / ExitPartial).
func newExplainCmd() *cobra.Command {
	fl := &runFlags{}
	cmd := &cobra.Command{
		Use:   "explain [FORMAT] [FILE...]",
		Short: "Dry-run mode: annotate every event with kept/dropped + reason.",
		Long: `explain runs the same pipeline as the run subcommand but
emits a diagnostic line per event instead of the distilled output.
Each line is one of:

  kept   <SEVERITY> <title> [at file:line] [<dedupe-evicted=K>] [<vendor-collapsed=N>] [<truncated>]
  dropped:<reason> <SEVERITY> <title> [at file:line]

Use this to understand why a given run produced the output it did
— particularly when --budget aggressively prunes events you
expected to see.`,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExplain(cmd, args, fl)
		},
	}
	registerRunFlags(cmd, fl)
	return cmd
}

// runExplain is the cobra RunE for the explain subcommand. It
// mirrors runRun's structure but constructs an ExplainSink and uses
// BuildExplain instead of Build so the instrumented BudgetStage
// records drops to the shared log.
func runExplain(cmd *cobra.Command, args []string, fl *runFlags) error {
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()
	stdin := cmd.InOrStdin()
	formatName, files := splitRunArgs(args)
	if !fl.autoDetect && formatName == "" {
		fmt.Fprintln(stderr, "distill-ai explain: --auto=false requires a positional FORMAT argument")
		return &exitCodeError{code: ExitError}
	}
	input, closer, sourceLabel, err := openRunInput(files, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai explain: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}
	lc := &output.LineCounter{Reader: input}
	cleaned, signals, stripper, err := envelope.Wrap(cmd.Context(), lc, envelope.Options{Choice: fl.stripEnvelope})
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai explain: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	if fl.verbose && stripper != nil && stripper.Name() != envelope.ChoiceNone {
		fmt.Fprintf(stderr, "distill-ai: envelope=%s\n", stripper.Name())
	}
	chosen, stream, sample, err := resolveFormat(cmd.Context(), formatName, cleaned, fl.strict)
	if err != nil {
		if errors.Is(err, detect.ErrNoFormat) {
			fmt.Fprintf(stderr, "distill-ai explain: no format matched %s\n", sourceLabel)
			fmt.Fprintln(stderr, "Hint: pass an explicit FORMAT argument, or rerun without --strict.")
			return &exitCodeError{code: ExitError}
		}
		fmt.Fprintf(stderr, "distill-ai explain: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	if fl.verbose {
		fmt.Fprintf(stderr, "distill-ai: explain format=%s source=%s sample_bytes=%d\n",
			chosen.Name(), sourceLabel, len(sample))
	}
	opts := buildPipelineOptions(fl)
	opts.EnvelopeSignals = signals
	parseOpts, err := buildParseOpts(fl)
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai explain: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	src := &pipeline.FormatSource{Format: chosen, Reader: stream, Opts: parseOpts}
	log := &pipeline.ExplainLog{}
	sink := &output.ExplainSink{
		Writer: stdout,
		Log:    log,
	}
	pipe, err := pipeline.BuildExplain(src, sink, opts, log)
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai explain: build pipeline: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := pipe.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return &exitCodeError{code: ExitError, cause: err}
		}
		fmt.Fprintf(stderr, "distill-ai explain: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	// Exit-code mapping uses the same precedence as run: forced
	// drops > no events > ok. The explain Sink emits something
	// even when every event is dropped, so we treat emitted==0
	// as "nothing kept" rather than ExitNoEvents — useful explain
	// runs may produce only dropped lines, and that is still
	// information.
	if pipe.BudgetCounters != nil && pipe.BudgetCounters.ForcedDrops() {
		return &exitCodeError{code: ExitPartial}
	}
	if sink.EventsEmitted() == 0 && log.Len() == 0 {
		return &exitCodeError{code: ExitNoEvents}
	}
	return nil
}
