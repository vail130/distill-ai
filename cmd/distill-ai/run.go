package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/output"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// runFlags collects every flag the run subcommand registers. Holding
// them in a struct rather than as cobra-local variables keeps the
// flag-registration code declarative and lets runRun read them from
// one place. The struct is built fresh per invocation by newRunCmd
// so successive test calls don't see each other's state.
type runFlags struct {
	// Input / format. autoDetect being false means "format must be
	// supplied as a positional argument".
	autoDetect  bool
	listFormats bool // M8.2 registers; plumbing deferred (lists registered formats and exits).

	// Filtering. M8.2 registers the flags; plumbing for severity /
	// max-events / context / keep-warnings is deferred to M8.2.x
	// (ParseOpts and Sink integration). keepVendor is plumbed via
	// pipeline.Options today.
	keepVendor   bool
	keepWarnings bool   // deferred plumbing
	severity     string // deferred plumbing
	maxEvents    int    // deferred plumbing
	context      int    // deferred plumbing

	// Deduplication. dedupeWindow=-1 means "let the run logic pick a
	// sensible default based on TTY / batch detection"; explicit
	// --dedupe-window=N overrides. --dedupe sets dedupeWindow to a
	// canonical default; --no-dedupe sets it to 0.
	dedupe       bool
	noDedupe     bool
	dedupeWindow int

	// Output.
	outputFormat    string
	outputStreaming bool
	budget          int
	noFooter        bool

	// Behaviour.
	explain     bool // deferred plumbing
	strict      bool
	passthrough bool // deferred plumbing
	tokenizer   string

	// Standard.
	verbose bool
}

// dedupeWindowDefault is the LRU capacity used when --dedupe is set
// without --dedupe-window. Matches the v1 sketch in ARCHITECTURE.md:
// a small window so streaming dedupe doesn't accumulate unbounded
// state across long-running tails.
const dedupeWindowDefault = 1024

// newRunCmd returns the cobra command for `distill-ai run`. The
// command also functions as the root's default (M8.2 wires it as the
// root's RunE so `cmd | distill-ai` works with no arguments).
func newRunCmd() *cobra.Command {
	fl := &runFlags{}
	cmd := &cobra.Command{
		Use:   "run [FORMAT] [FILE...]",
		Short: "Distill input through the format pipeline and emit a compact summary.",
		Long: `run is the default subcommand: it reads from stdin (or
positional FILE arguments), detects or accepts an explicit format,
runs the distillation pipeline, and emits the result to stdout.

When FORMAT is omitted (the default), the autodetector picks the
best-scoring format from the first 4 KiB of input. Pass FORMAT
explicitly to skip detection — useful when the source format is
known or when --strict would otherwise reject ambiguous input.

Multiple FILEs are concatenated with a newline separator and parsed
as a single stream. Mixed-format inputs are not yet supported; run
once per format.`,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(cmd, args, fl)
		},
	}
	registerRunFlags(cmd, fl)
	return cmd
}

// registerRunFlags binds every flag described in
// ARCHITECTURE.md § Flags to the runFlags struct. Each call here is
// matched by either active use in runRun or by a documented
// "plumbing deferred to M8.2.x" comment on the field above.
func registerRunFlags(cmd *cobra.Command, fl *runFlags) {
	// Input / format.
	cmd.Flags().BoolVar(&fl.autoDetect, "auto", true,
		"Autodetect the input format from a sample of the input. Pass --auto=false with a positional FORMAT to skip detection.")
	cmd.Flags().BoolVar(&fl.listFormats, "list-formats", false,
		"List every registered format and exit. (Equivalent to the 'list-formats' subcommand; plumbing lands in M8.4.)")
	// Filtering.
	cmd.Flags().BoolVar(&fl.keepVendor, "keep-vendor", false,
		"Leave vendor / stdlib stack frames in the output instead of collapsing them.")
	cmd.Flags().BoolVar(&fl.keepWarnings, "keep-warnings", false,
		"Include warnings alongside errors. Per-format opt-in; honoured by 'generic' as of M9.4.")
	cmd.Flags().StringVar(&fl.severity, "severity", "",
		"Minimum severity to keep (error|warn|info). Empty means format-default (error). Per-format opt-in; honoured by 'generic' as of M9.4.")
	cmd.Flags().IntVar(&fl.maxEvents, "max-events", 0,
		"Cap the total number of events emitted. Zero means no cap. (Plumbing lands in M8.2.x.)")
	cmd.Flags().IntVar(&fl.context, "context", 0,
		"Lines of context around each event. Zero means format-default (3 for 'generic').")
	// Deduplication.
	cmd.Flags().BoolVar(&fl.dedupe, "dedupe", false,
		"Enable LRU deduplication of identical events. Default window is 1024; override with --dedupe-window.")
	cmd.Flags().BoolVar(&fl.noDedupe, "no-dedupe", false,
		"Disable LRU deduplication. Wins over --dedupe when both are set.")
	cmd.Flags().IntVar(&fl.dedupeWindow, "dedupe-window", -1,
		"LRU capacity for dedupe. Negative means 'pick a default based on --dedupe / --no-dedupe'; 0 disables dedupe; positive enables with that capacity.")
	// Output.
	cmd.Flags().StringVar(&fl.outputFormat, "output", "text",
		"Output encoder: text | json | markdown.")
	cmd.Flags().BoolVar(&fl.outputStreaming, "output-streaming", false,
		"Emit ndjson with a trailing summary line instead of a single batch object. Only affects --output=json.")
	cmd.Flags().IntVar(&fl.budget, "budget", 0,
		"Target output token cost. Zero means no cap. Drops or truncates lower-severity events to fit; exit code 3 reports drops.")
	cmd.Flags().BoolVar(&fl.noFooter, "no-footer", false,
		"Suppress the trailing 'collapsed X, dropped Y' summary. (Ignored by --output=json; the summary is part of the schema.)")
	// Behaviour.
	cmd.Flags().BoolVar(&fl.explain, "explain", false,
		"Dry-run mode: annotate which events were kept or dropped and why, without distilled output. Equivalent to the 'explain' subcommand; the flag is registered for convenience but the subcommand is preferred.")
	cmd.Flags().BoolVar(&fl.strict, "strict", false,
		"Fail with exit code 2 when autodetect can't pick a specific format with confidence ≥ 0.6.")
	cmd.Flags().BoolVar(&fl.passthrough, "passthrough", false,
		"If no events were found, emit the input unchanged instead of an empty stream. (Plumbing lands in M8.2.x.)")
	cmd.Flags().StringVar(&fl.tokenizer, "tokenizer", "heuristic",
		"Token estimator used by --budget: heuristic | tiktoken. heuristic is fast and zero-dep; tiktoken is exact for OpenAI / Claude models.")
	// Standard.
	cmd.Flags().BoolVarP(&fl.verbose, "verbose", "v", false,
		"Write pipeline diagnostics to stderr (chosen format, sample bytes consumed, per-stage event counts).")
}

// runRun is the cobra RunE entry point for the run subcommand. It
// resolves input, picks a format, builds the pipeline, executes it,
// and translates the result into an exit code.
func runRun(cmd *cobra.Command, args []string, fl *runFlags) error {
	if fl.listFormats {
		// --list-formats is a shortcut for the list-formats
		// subcommand. Delegate to its implementation so both code
		// paths produce byte-identical output.
		return runListFormats(cmd, nil)
	}
	if fl.explain {
		// --explain is a shortcut for the explain subcommand.
		// Delegate so both spellings produce identical output.
		return runExplain(cmd, args, fl)
	}
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()
	stdin := cmd.InOrStdin()
	formatName, files := splitRunArgs(args)
	if !fl.autoDetect && formatName == "" {
		fmt.Fprintln(stderr, "distill-ai run: --auto=false requires a positional FORMAT argument")
		return &exitCodeError{code: ExitError}
	}
	input, closer, sourceLabel, err := openRunInput(files, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai run: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}
	// Install a LineCounter around the input so the Sink footer can
	// report the input-line count.
	lc := &output.LineCounter{Reader: input}
	chosen, stream, sample, err := resolveFormat(cmd.Context(), formatName, lc, fl.strict)
	if err != nil {
		if errors.Is(err, detect.ErrNoFormat) {
			fmt.Fprintf(stderr, "distill-ai run: no format matched %s\n", sourceLabel)
			fmt.Fprintln(stderr, "Hint: pass an explicit FORMAT argument, or rerun without --strict.")
			return &exitCodeError{code: ExitError}
		}
		fmt.Fprintf(stderr, "distill-ai run: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	if fl.verbose {
		fmt.Fprintf(stderr, "distill-ai: format=%s source=%s sample_bytes=%d\n",
			chosen.Name(), sourceLabel, len(sample))
	}
	opts := buildPipelineOptions(fl)
	parseOpts, err := buildParseOpts(fl)
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai run: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	src := &pipeline.FormatSource{Format: chosen, Reader: stream, Opts: parseOpts}
	sink, sinkInfo := newSinkFromFlags(fl, chosen.Name(), stdout)
	pipe, err := pipeline.Build(src, sink, opts)
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai run: build pipeline: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	// Wire the BudgetCounters from Build into the Sink so the Sink
	// can include drop / token stats in its footer. The Counters
	// pointer is shared with the BudgetStage; the Sink reads it
	// after the stage has populated it (at end of pipeline).
	attachCounters(sink, pipe.BudgetCounters)
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := pipe.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return &exitCodeError{code: ExitError, cause: err}
		}
		fmt.Fprintf(stderr, "distill-ai run: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	// Finalise: install the input line count and budget counters on
	// the Sink so its footer (text / markdown) or trailer (json) is
	// accurate. The Sink already wrote its body; these two reads
	// matter only for the trailing summary path on the JSON encoder
	// in batch mode, which writes after Sink.Sink returns... no, in
	// the current Sink shape the footer is written inside Sink.Sink,
	// so reading these here is a no-op for text/markdown. M8.2 sets
	// them before Run so the post-Run snapshot is consistent for
	// callers that grab pipe.BudgetCounters.
	_ = sinkInfo
	// Exit-code precedence (see ARCHITECTURE.md Exit codes):
	//
	//   3 — forced drops or truncations under --budget. Wins over
	//       exit 1 because an empty output caused by an aggressive
	//       budget is meaningfully different from "input was clean".
	//   1 — pipeline succeeded but emitted zero events.
	//   0 — pipeline succeeded with at least one event.
	if pipe.BudgetCounters != nil && pipe.BudgetCounters.ForcedDrops() {
		return &exitCodeError{code: ExitPartial}
	}
	if readEmitted(sink) == 0 {
		return &exitCodeError{code: ExitNoEvents}
	}
	return nil
}

// sinkInfo gives the run() caller enough context about the Sink to
// finalise its trailing summary (line counts, estimator name). Today
// every Sink reads its inputs at construction; this struct is a
// placeholder for M8.3's exit-code threading where JSONSink.ExitCode
// needs to be written between Pipeline.Run and the Sink's trailer.
type sinkInfo struct {
	emittedFn func() int
}

// newSinkFromFlags builds the appropriate Sink for --output / --no-footer.
// Returns the Sink as a pipeline.Sink (since that's what Build wants)
// plus a sinkInfo carrying the typed handle the caller needs for
// post-Run bookkeeping.
func newSinkFromFlags(fl *runFlags, formatName string, w io.Writer) (pipeline.Sink, sinkInfo) {
	switch strings.ToLower(fl.outputFormat) {
	case "json":
		s := &output.JSONSink{
			Writer:        w,
			NoFooter:      fl.noFooter,
			FormatName:    formatName,
			Streaming:     fl.outputStreaming,
			EstimatorName: fl.tokenizer,
		}
		return s, sinkInfo{emittedFn: s.EventsEmitted}
	case "markdown":
		s := &output.MarkdownSink{
			Writer:        w,
			NoFooter:      fl.noFooter,
			FormatName:    formatName,
			EstimatorName: fl.tokenizer,
		}
		return s, sinkInfo{emittedFn: s.EventsEmitted}
	default:
		// text is the documented default; an unknown value also
		// falls through here. M8.2 keeps the silent-fallback shape;
		// a future commit can promote unknown values to an error.
		s := &output.TextSink{
			Writer:        w,
			NoFooter:      fl.noFooter,
			FormatName:    formatName,
			EstimatorName: fl.tokenizer,
		}
		return s, sinkInfo{emittedFn: s.EventsEmitted}
	}
}

// readEmitted dispatches on Sink concrete type to read its event
// count. Every M7 Sink exposes an EventsEmitted() int method but the
// pipeline.Sink interface doesn't require it; readEmitted bridges
// the two without forcing a new method on the interface.
func readEmitted(s pipeline.Sink) int {
	type emittedReader interface{ EventsEmitted() int }
	if r, ok := s.(emittedReader); ok {
		return r.EventsEmitted()
	}
	return 0
}

// attachCounters sets the BudgetCounters pointer on the Sink so its
// footer (text / markdown) or summary (json) can include drop and
// token statistics. Type-asserts on the concrete M7 Sink shape
// rather than adding a method to the pipeline.Sink interface,
// since not every future Sink will care.
func attachCounters(s pipeline.Sink, c *pipeline.BudgetCounters) {
	if c == nil {
		return
	}
	switch sink := s.(type) {
	case *output.TextSink:
		sink.Counters = c
	case *output.JSONSink:
		sink.Counters = c
	case *output.MarkdownSink:
		sink.Counters = c
	}
}

// buildParseOpts translates the parsing-related runFlags into a
// formats.ParseOpts value forwarded to Format.Parse via
// FormatSource.Opts. Returns an error for invalid --severity values
// so the user sees a clear message rather than a silent "default
// took effect" surprise.
func buildParseOpts(fl *runFlags) (formats.ParseOpts, error) {
	opts := formats.ParseOpts{
		ContextLines: fl.context,
		KeepVendor:   fl.keepVendor,
		KeepWarnings: fl.keepWarnings,
	}
	if fl.severity != "" {
		sev, err := event.ParseSeverity(fl.severity)
		if err != nil {
			return formats.ParseOpts{}, fmt.Errorf("invalid --severity %q (want error|warn|info)", fl.severity)
		}
		opts.MinSeverity = sev
	}
	return opts, nil
}

// buildPipelineOptions translates runFlags into pipeline.Options. The
// dedupe flag triplet (--dedupe / --no-dedupe / --dedupe-window) is
// resolved here so runRun stays declarative.
func buildPipelineOptions(fl *runFlags) pipeline.Options {
	opts := pipeline.Options{
		KeepVendor: fl.keepVendor,
		Budget:     fl.budget,
		Tokenizer:  fl.tokenizer,
	}
	opts.DedupeWindow = resolveDedupeWindow(fl)
	return opts
}

// resolveDedupeWindow implements the precedence rule:
//
//   - --no-dedupe wins over --dedupe and over --dedupe-window > 0.
//   - --dedupe-window with a positive value wins over --dedupe.
//   - --dedupe-window=0 disables dedupe (parity with --no-dedupe).
//   - --dedupe alone uses dedupeWindowDefault.
//   - Default (nothing set): 0 (dedupe off).
func resolveDedupeWindow(fl *runFlags) int {
	if fl.noDedupe {
		return 0
	}
	if fl.dedupeWindow >= 0 {
		return fl.dedupeWindow
	}
	if fl.dedupe {
		return dedupeWindowDefault
	}
	return 0
}

// splitRunArgs separates the positional FORMAT from the FILE args.
// The convention: if the first positional matches a registered
// format name, treat it as FORMAT; otherwise treat all positionals
// as FILEs and rely on autodetect.
//
// This is convenient at the cost of one edge case: a file whose
// name happens to match a format name would be misclassified. In
// practice users with such a file would pass `--auto` plus the
// file, or rename it. The trade-off favours the common case
// (`cmd | distill-ai pytest`) over the rare edge case.
func splitRunArgs(args []string) (format string, files []string) {
	if len(args) == 0 {
		return "", nil
	}
	if _, ok := formats.Get(args[0]); ok {
		return args[0], args[1:]
	}
	return "", args
}

// openRunInput resolves the FILE arguments into a single io.Reader.
// When files is empty, stdin is used. Multiple files are
// concatenated with a newline separator so a single format pass
// handles the whole stream.
func openRunInput(files []string, stdin io.Reader) (r io.Reader, closer io.Closer, source string, err error) {
	if len(files) == 0 {
		return stdin, nil, "stdin", nil
	}
	if len(files) == 1 && files[0] == "-" {
		return stdin, nil, "stdin", nil
	}
	if len(files) == 1 {
		f, err := os.Open(files[0]) //nolint:gosec // G304 user-provided path is the point
		if err != nil {
			return nil, nil, files[0], err
		}
		return f, f, files[0], nil
	}
	// Multi-file: open each, concatenate via io.MultiReader with a
	// newline separator between them so format detection sees a
	// single stream. Return a multiCloser so all handles close on
	// completion.
	readers := make([]io.Reader, 0, 2*len(files))
	closers := make([]io.Closer, 0, len(files))
	for i, name := range files {
		f, err := os.Open(name) //nolint:gosec // G304 user-provided path is the point
		if err != nil {
			for _, c := range closers {
				_ = c.Close()
			}
			return nil, nil, name, err
		}
		if i > 0 {
			readers = append(readers, strings.NewReader("\n"))
		}
		readers = append(readers, f)
		closers = append(closers, f)
	}
	return io.MultiReader(readers...), &multiCloser{closers: closers}, strings.Join(files, ","), nil
}

// multiCloser closes a list of io.Closers in order, returning the
// first error encountered while still attempting the rest. Used by
// the multi-file run input path.
type multiCloser struct {
	closers []io.Closer
}

func (m *multiCloser) Close() error {
	var firstErr error
	for _, c := range m.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// resolveFormat picks the Format that should parse the input. When
// formatName is non-empty (explicit FORMAT positional), look it up
// in the registry and use it; the full input is the stream. When
// formatName is empty, run the autodetector against a sample and
// hand back its winner along with the prepended stream.
//
// Returns the chosen Format, the stream the pipeline should parse,
// the sample the detector consumed (empty when format was explicit),
// and any error.
func resolveFormat(ctx context.Context, formatName string, r io.Reader, strict bool) (formats.Format, io.Reader, []byte, error) {
	if formatName != "" {
		f, ok := formats.Get(formatName)
		if !ok {
			return nil, nil, nil, fmt.Errorf("unknown format %q (use --list-formats to see registered formats)", formatName)
		}
		return f, r, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := detect.Detect(ctx, r, detect.Opts{Strict: strict})
	if err != nil {
		return nil, nil, nil, err
	}
	return res.Format, res.Stream, res.Sample, nil
}
