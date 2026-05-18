// Package orchestrator is the private bridge between the public
// pkg/distill API and the internal/ packages it composes.
//
// Layering: pkg/distill is the publicly importable library surface.
// Internal/ packages (internal/pipeline, internal/output,
// internal/detect, internal/envelope, internal/tokens) are
// implementation detail. Go's `internal/` visibility rule keeps
// this package unreachable from anywhere except pkg/distill and
// its subpackages, so it can freely import every internal/
// package without leaking those imports into the public surface.
//
// The Config type is a flat, intermediate representation: pkg/distill
// translates its public Options into a Config and hands it to
// New, which sets up the run. Decoupling the public Options from the
// orchestrator's internal vocabulary means future internal refactors
// (renaming a Stage, changing an option's units, swapping the Sink
// constructor signature) don't ripple into pkg/distill's published
// godoc.
//
// This package is internal/ on purpose. Do not export anything from
// here that isn't required by pkg/distill itself.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/output"
	"github.com/vail130/distill-ai/internal/pipeline"
	"github.com/vail130/distill-ai/internal/tokens"
)

// Sentinel errors returned by Setup. pkg/distill wraps these into
// its own ErrNilWriter / ErrUnknownTokenizer / etc. errors so the
// public surface keeps a clean named-error set.
var (
	// ErrNilWriter is returned when Config.Writer is nil.
	ErrNilWriter = errors.New("orchestrator: Writer is nil")

	// ErrNilReader is returned when the Reader passed to Setup is
	// nil. Distinct from ErrNilWriter so pkg/distill can map it.
	ErrNilReader = errors.New("orchestrator: Reader is nil")

	// ErrUnknownTokenizer is returned when Config.Tokenizer is not
	// "heuristic", "tiktoken", or empty.
	ErrUnknownTokenizer = errors.New("orchestrator: unknown Tokenizer")

	// ErrUnknownFormat is returned when Config.Format names a
	// format that is not registered.
	ErrUnknownFormat = errors.New("orchestrator: unknown Format")

	// ErrUnknownOutput is returned when Config.Output is not one
	// of the documented OutputFormat values.
	ErrUnknownOutput = errors.New("orchestrator: unknown Output")

	// ErrUnknownStripEnvelope is returned when Config.StripEnvelope
	// names an envelope stripper that is not registered.
	ErrUnknownStripEnvelope = errors.New("orchestrator: unknown StripEnvelope")
)

// Output enumerates the encoder choices Setup knows about. The
// numeric values are not stable; the constants are. pkg/distill
// maps its public OutputFormat strings onto these.
type Output int

const (
	// OutputText is the compact text encoder. Default.
	OutputText Output = iota

	// OutputJSON is the batch JSON encoder.
	OutputJSON

	// OutputJSONStreaming is the ndjson encoder.
	OutputJSONStreaming

	// OutputMarkdown is the markdown encoder.
	OutputMarkdown
)

// Config is the internal vocabulary Setup consumes. Fields are
// semantically identical to pkg/distill.Options but expressed in
// the internal types (Output is a typed integer, not a string;
// MinSeverity is event.Severity directly; etc.) so the
// orchestrator doesn't have to re-parse public string values on
// every call.
type Config struct {
	Format        string
	Strict        bool
	Output        Output
	Budget        int
	Tokenizer     string
	DedupeWindow  int
	KeepVendor    bool
	KeepWarnings  bool
	MinSeverity   event.Severity
	MaxEvents     int
	ContextLines  int
	StripEnvelope string
	Writer        io.Writer
	NoFooter      bool
	FenceLang     string
}

// Run captures everything Setup wired up. The caller invokes Start
// (which begins the pipeline goroutine and returns the Event
// channel) and Wait (which blocks for completion and returns the
// Summary).
//
// Separating Start and Wait lets the library caller stream Events
// to its own consumer while the pipeline runs, then read the
// Summary once the channel closes — matching the documented
// "fields valid only after Event channel closes" contract.
//
// The Event channel returned by Start receives every Event the
// pipeline produces post-stages (after Collapse, Dedupe, Budget,
// MaxEvents) and pre-Sink-encoding. Library callers that want
// programmatic access to Events read from the channel; library
// callers that only want the encoded output through Writer can
// drain the channel with a no-op goroutine, ignoring its content.
// Either way the Sink still writes to Writer in parallel.
type Run struct {
	// pipeline is the assembled pipeline.Pipeline ready to Run.
	pipeline *pipeline.Pipeline

	// sink is the underlying encoder Sink. The pipeline drives a
	// teeing wrapper that fans Events to both this Sink and the
	// public Event channel.
	sink pipeline.Sink

	// lineCounter wraps the input Reader to count input lines for
	// the Summary's InputLines field.
	lineCounter *output.LineCounter

	// estimatorName is "heuristic" or "tiktoken", recorded so the
	// Summary's Estimator field is accurate.
	estimatorName string

	// events is the channel published to the library caller. The
	// teeingSink pushes each Event here before forwarding to the
	// underlying Sink, so a slow caller backpressures the entire
	// pipeline.
	events chan event.Event

	// done is closed by Start's runner goroutine after pipeline.Run
	// returns. Wait blocks on this.
	done chan struct{}

	// runErr is the error returned by pipeline.Run. Read after
	// done closes.
	runErr error
}

// teeingSink fans each Event to a channel and to an underlying
// Sink in parallel. The channel send happens first so the caller's
// consumer applies backpressure to the pipeline; the underlying
// Sink then encodes the Event to its writer. Both operations honour
// ctx.
type teeingSink struct {
	out  chan<- event.Event
	sink pipeline.Sink
}

// Sink implements pipeline.Sink. Each Event read from in is sent on
// out (so the public channel sees it) and forwarded into a relay
// channel the underlying Sink drains. When in closes, the relay
// closes, the underlying Sink returns, and out closes.
func (t teeingSink) Sink(ctx context.Context, in <-chan event.Event) error {
	relay := make(chan event.Event, cap(in))
	sinkErr := make(chan error, 1)
	go func() {
		sinkErr <- t.sink.Sink(ctx, relay)
	}()
	defer func() {
		close(relay)
		// Drain the underlying Sink's error so the goroutine exits.
		<-sinkErr
		close(t.out)
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return nil
			}
			// Push to the public channel first so a slow caller
			// backpressures the pipeline; if ctx cancels while
			// we're blocked, return cleanly.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case t.out <- ev:
			}
			// Then forward to the underlying Sink. The relay
			// channel is buffered to BufferSize so this rarely
			// blocks.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case relay <- ev:
			}
		}
	}
}

// EventsEmitted exposes the underlying Sink's count so Run.summary
// can read it through the teeingSink without a type assertion.
func (t teeingSink) EventsEmitted() int {
	if e, ok := t.sink.(interface{ EventsEmitted() int }); ok {
		return e.EventsEmitted()
	}
	return 0
}

// Summary mirrors pkg/distill.Summary. The Run.Wait return value
// translates into the public type without leaking internal types.
type Summary struct {
	InputLines          int
	OutputLines         int
	EventsFound         int
	EventsEmitted       int
	EventsDeduped       int
	EventsDroppedBudget int
	EventsTruncated     int
	FramesCollapsed     int
	EstimatedTokens     int
	Estimator           string
	ExitCode            int
}

// Setup validates Config, resolves format and envelope, builds the
// pipeline, and returns a Run ready for Start/Wait. Errors are
// returned synchronously before any goroutine starts so the caller
// sees deterministic setup failures.
func Setup(ctx context.Context, cfg Config, r io.Reader) (*Run, error) {
	if r == nil {
		return nil, ErrNilReader
	}
	if cfg.Writer == nil {
		return nil, ErrNilWriter
	}
	// Validate tokenizer before any pipeline goroutine starts. The
	// error message intentionally names the offending value so the
	// caller's error surface is useful.
	estimatorName := cfg.Tokenizer
	if estimatorName == "" {
		estimatorName = "heuristic"
	}
	if _, err := tokens.ByName(estimatorName); err != nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTokenizer, cfg.Tokenizer)
	}
	// LineCounter wraps the input Reader so the Sink footer can
	// report input_lines accurately.
	lc := &output.LineCounter{Reader: r}
	// Envelope stripping runs before format detection so the
	// detector sees cleaned bytes. The envelope.Wrap helper validates
	// the StripEnvelope choice and returns the cleaned Reader plus
	// a signals channel that fans into the pipeline.
	cleaned, signals, _, err := envelope.Wrap(ctx, lc, envelope.Options{Choice: cfg.StripEnvelope})
	if err != nil {
		// envelope.Wrap returns its own typed error for unknown
		// choices; surface it as ErrUnknownStripEnvelope so
		// pkg/distill can map it cleanly without import-coupling.
		if errors.Is(err, envelope.ErrUnknownStripper) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownStripEnvelope, cfg.StripEnvelope)
		}
		return nil, fmt.Errorf("orchestrator: envelope: %w", err)
	}
	// Format resolution. Explicit cfg.Format wins; otherwise run
	// the autodetector with the cfg.Strict flag.
	chosenFormat, stream, err := resolveFormat(ctx, cfg.Format, cleaned, cfg.Strict)
	if err != nil {
		return nil, err
	}
	// Build per-format parse options.
	parseOpts := formats.ParseOpts{
		ContextLines: cfg.ContextLines,
		KeepVendor:   cfg.KeepVendor,
		KeepWarnings: cfg.KeepWarnings,
		MinSeverity:  cfg.MinSeverity,
	}
	src := &pipeline.FormatSource{Format: chosenFormat, Reader: stream, Opts: parseOpts}
	// Build the encoder Sink, then wrap it in a teeingSink so the
	// pipeline drives both the encoder (writing to cfg.Writer) and
	// the public Event channel returned by Start. The teeingSink
	// owns closing the public channel when the pipeline drains.
	innerSink, err := buildSink(cfg, chosenFormat.Name(), estimatorName)
	if err != nil {
		return nil, err
	}
	events := make(chan event.Event, pipeline.DefaultBufferSize)
	wrappedSink := teeingSink{out: events, sink: innerSink}
	pipeOpts := pipeline.Options{
		DedupeWindow:    cfg.DedupeWindow,
		KeepVendor:      cfg.KeepVendor,
		Budget:          cfg.Budget,
		Tokenizer:       estimatorName,
		MaxEvents:       cfg.MaxEvents,
		EnvelopeSignals: signals,
	}
	pipe, err := pipeline.Build(src, wrappedSink, pipeOpts)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: build pipeline: %w", err)
	}
	// Wire BudgetCounters into the underlying Sink so its footer
	// reflects drop / token statistics. The teeingSink is just a
	// fan-out; the encoded output and the counters live on the
	// inner Sink.
	attachCounters(innerSink, pipe.BudgetCounters)
	return &Run{
		pipeline:      pipe,
		sink:          innerSink,
		lineCounter:   lc,
		estimatorName: estimatorName,
		events:        events,
		done:          make(chan struct{}),
	}, nil
}

// resolveFormat picks the Format that should parse the stream. An
// explicit format name bypasses detection; otherwise the detector
// runs.
func resolveFormat(ctx context.Context, name string, r io.Reader, strict bool) (formats.Format, io.Reader, error) {
	if name != "" {
		f, ok := formats.Get(name)
		if !ok {
			return nil, nil, fmt.Errorf("%w: %q", ErrUnknownFormat, name)
		}
		return f, r, nil
	}
	res, err := detect.Detect(ctx, r, detect.Opts{Strict: strict})
	if err != nil {
		return nil, nil, fmt.Errorf("orchestrator: detect: %w", err)
	}
	return res.Format, res.Stream, nil
}

// buildSink constructs the requested encoder. The Sink is configured
// with FormatName, EstimatorName, NoFooter, and FenceLang (for
// Markdown only). BudgetCounters and Streaming/InputLines wiring
// happens later — BudgetCounters via attachCounters after Build,
// Streaming is part of the Output enum, and InputLines reads off
// the LineCounter after the pipeline drains.
func buildSink(cfg Config, formatName, estimatorName string) (pipeline.Sink, error) {
	switch cfg.Output {
	case OutputText:
		return &output.TextSink{
			Writer:        cfg.Writer,
			NoFooter:      cfg.NoFooter,
			FormatName:    formatName,
			EstimatorName: estimatorName,
		}, nil
	case OutputJSON:
		return &output.JSONSink{
			Writer:        cfg.Writer,
			NoFooter:      cfg.NoFooter,
			FormatName:    formatName,
			EstimatorName: estimatorName,
			Streaming:     false,
		}, nil
	case OutputJSONStreaming:
		return &output.JSONSink{
			Writer:        cfg.Writer,
			NoFooter:      cfg.NoFooter,
			FormatName:    formatName,
			EstimatorName: estimatorName,
			Streaming:     true,
		}, nil
	case OutputMarkdown:
		return &output.MarkdownSink{
			Writer:        cfg.Writer,
			NoFooter:      cfg.NoFooter,
			FormatName:    formatName,
			EstimatorName: estimatorName,
			FenceLang:     cfg.FenceLang,
		}, nil
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnknownOutput, cfg.Output)
	}
}

// attachCounters sets the BudgetCounters pointer on the Sink so its
// footer can include drop / token statistics. Type-asserts on each
// concrete Sink rather than adding a method to the pipeline.Sink
// interface.
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

// Start launches the pipeline in a goroutine and returns the Event
// channel. The pipeline runs to completion in the background; the
// caller reads from the channel for programmatic access, and / or
// invokes Wait to block until it finishes and receive the Summary.
//
// The returned channel closes when the pipeline drains (whether
// because the input EOF'd, the ctx was cancelled, or an error
// occurred). A caller that doesn't care about programmatic Event
// access can drain the channel with a no-op goroutine, ignoring
// every Event; the encoder Sink writes to cfg.Writer in parallel
// either way.
//
// Start must be called exactly once per Run. Calling it twice is
// a programmer error and may panic.
func (r *Run) Start(ctx context.Context) <-chan event.Event {
	go func() {
		defer close(r.done)
		r.runErr = r.pipeline.Run(ctx)
	}()
	return r.events
}

// Wait blocks until the pipeline finishes and returns the Summary
// plus any error pipeline.Run returned. The Summary is valid only
// after Wait returns — reading orchestrator state mid-run is
// undefined.
//
// Wait must be called exactly once per Run, after Start. Calling
// Wait without Start blocks forever.
func (r *Run) Wait() (*Summary, error) {
	<-r.done
	return r.summary(), r.runErr
}

// summary materialises a Summary from the pipeline's BudgetCounters,
// the Sink's EventsEmitted method, and the LineCounter's Lines
// value. Called by Wait once the pipeline has drained.
func (r *Run) summary() *Summary {
	s := &Summary{
		Estimator: r.estimatorName,
	}
	if r.lineCounter != nil {
		s.InputLines = r.lineCounter.Lines()
	}
	if c := r.pipeline.BudgetCounters; c != nil {
		s.EventsDroppedBudget = c.EventsDroppedBudget
		s.EventsTruncated = c.EventsTruncated
		s.EstimatedTokens = c.EstimatedTokens
	}
	if emitter, ok := r.sink.(interface{ EventsEmitted() int }); ok {
		s.EventsEmitted = emitter.EventsEmitted()
	}
	// EventsFound is best-effort: the parsers don't expose a
	// before-pipeline count, so we report EventsEmitted plus the
	// budget-stage drops. EventsDeduped and FramesCollapsed are
	// likewise not tracked centrally; M15.2 can extend the Sink
	// interface in a follow-up if these need to be exact. For now
	// they remain zero, mirroring the JSON encoder's behaviour
	// when no BudgetStage runs.
	s.EventsFound = s.EventsEmitted + s.EventsDroppedBudget
	// ExitCode mirrors the CLI's M8.3 mapping. The library helper
	// pkg/distill.ExitCodeFromSummary re-implements this for
	// callers that want to react to it; populating it here keeps
	// the Summary self-describing for JSON consumers.
	switch {
	case s.EventsDroppedBudget > 0 || s.EventsTruncated > 0:
		s.ExitCode = 3
	case s.EventsEmitted == 0:
		s.ExitCode = 1
	default:
		s.ExitCode = 0
	}
	return s
}
