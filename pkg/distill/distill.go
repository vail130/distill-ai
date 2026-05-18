// Package distill is the stable public library API for distill-ai.
//
// Most users invoke the CLI binary (`distill-ai`) directly. This
// package exists for code that wants to embed the distillation
// pipeline as a library: a custom tool, a server, a test runner,
// an MCP server, an editor integration.
//
// # Surface
//
// The library API is intentionally narrow. There is one entry point
// and four types:
//
//   - [Distill] — the streaming entry point. Reads from an
//     io.Reader, emits Events on a channel, populates a *Summary
//     when the channel closes.
//   - [Options] — the one struct callers fill in. Mirrors the CLI
//     flags but is decoupled from internal/pipeline so future
//     refactoring doesn't break callers.
//   - [Summary] — the run-level counters. Same numbers a JSON
//     consumer of `distill-ai run --output=json` would see.
//   - [Event], [Severity], [Location], [StackFrame],
//     [Confidence] — re-exported core types. Same shape as the
//     internal event package.
//
// # Config files
//
// Config-file loading is deliberately not part of this package.
// `.distill-ai.toml` is a CLI concern; library callers compose
// their own [Options]. See docs/library-api.md for the rationale.
//
// # Versioning
//
// This package is the project's public Go API surface. Breaking
// changes follow the project's SemVer commitment (see CHANGELOG.md
// and [output-stability rule]); callers should pin to a major
// version (`v1`) and review the CHANGELOG before bumping.
//
// See ARCHITECTURE.md § Library API for the design intent and
// CHANGELOG.md for the public API timeline.
//
// [output-stability rule]: https://github.com/vail130/distill-ai/blob/main/rules/output-stability.md
package distill

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/pkg/distill/internal/orchestrator"
)

// Event is the unit of distillation. See event.Event for the full
// godoc, including the public JSON-schema contract.
type Event = event.Event

// Severity classifies an Event. See event.Severity.
type Severity = event.Severity

// Severity constants re-exported for caller convenience.
const (
	SeverityError = event.SeverityError
	SeverityWarn  = event.SeverityWarn
	SeverityInfo  = event.SeverityInfo
)

// Location identifies a source position attached to an Event.
type Location = event.Location

// StackFrame is one frame of a structured stack trace.
type StackFrame = event.StackFrame

// Confidence is a detector's self-reported certainty in [0.0, 1.0].
type Confidence = event.Confidence

// ConfidenceMinDetect is the threshold below which the detector falls
// back to the generic format.
const ConfidenceMinDetect = event.ConfidenceMinDetect

// Format is the parser plugin contract. See formats.Format.
type Format = formats.Format

// ParseOpts carries per-invocation parsing options.
type ParseOpts = formats.ParseOpts

// Distill is the library entry point. It reads from r, runs the
// distillation pipeline configured by opts, writes encoded output
// to opts.Writer, and returns a channel of Events plus a *Summary
// the caller can read after the channel closes.
//
// # Streaming
//
// The returned channel publishes every Event the pipeline emits
// post-stages (after collapse, dedupe, budget, max-events) and
// pre-encoding. Library callers that want programmatic access to
// individual Events read from the channel; callers that only want
// the encoded output through opts.Writer drain the channel with a
// no-op goroutine, ignoring its content. Either way the encoder
// writes to opts.Writer in parallel.
//
// The channel is buffered (capacity matches the pipeline's
// DefaultBufferSize). A slow consumer applies backpressure to the
// entire pipeline.
//
// # Summary timing
//
// The returned *Summary is non-nil on a successful Distill call,
// but its fields are valid only after the Event channel closes.
// Reading Summary fields while events are still flowing returns
// in-flight values that may not match the final state. A typical
// pattern:
//
//	events, summary, err := distill.Distill(ctx, r, opts)
//	if err != nil { ... }
//	for ev := range events { ... }
//	// Now summary fields are valid.
//	if summary.ForcedDrops() { ... }
//
// # Errors
//
// Distill returns a non-nil error for setup failures only:
// ErrNilWriter when opts.Writer is nil; ErrUnknownTokenizer,
// ErrUnknownOutput, ErrUnknownFormat, or ErrUnknownStripEnvelope
// when the corresponding opts.* field names an unrecognised value.
// All four are returned synchronously before any goroutine starts;
// the caller never sees the channel or the Summary in these cases.
//
// Mid-stream parser errors do NOT surface as a Distill return
// value. Per the project's resolution recorded in
// KNOWN_ISSUES.md, parsers convert recoverable problems into
// best-effort Events and continue; unrecoverable problems close
// the channel cleanly and degrade to whatever Events were emitted
// before the failure. A library caller that wants to detect such
// degradations should inspect the Summary or use OutputJSONStreaming
// and parse the trailer.
//
// # Cancellation
//
// A cancelled ctx propagates to every pipeline goroutine; the
// Event channel closes, the Summary populates with whatever the
// pipeline had observed at cancellation time, and Distill's
// goroutines exit cleanly. The library caller is responsible for
// closing r if r is an io.Closer.
//
// # Format and envelope registration
//
// Importing this package brings the full v1 format set (generic,
// gotest, jest, pytest) and envelope strippers (github-actions,
// gitlab-ci) into the global registry via side-effect imports in
// register.go. Callers therefore get the same default behaviour
// as the CLI without enumerating each internal/formats/* package.
func Distill(ctx context.Context, r io.Reader, opts Options) (<-chan Event, *Summary, error) {
	if opts.Writer == nil {
		return nil, nil, ErrNilWriter
	}
	cfg, err := translateOptions(opts)
	if err != nil {
		return nil, nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	run, err := orchestrator.Setup(ctx, cfg, r)
	if err != nil {
		return nil, nil, translateOrchestratorError(err)
	}
	channel := run.Start(ctx)
	// summary is populated by a goroutine that blocks on run.Wait()
	// and copies the internal summary into the public Summary the
	// caller already holds a pointer to. The summary's done channel
	// closes after the copy so callers that consult Summary fields
	// have a happens-before edge they can synchronise on; reading
	// fields before Wait returns or Done closes is a race.
	summary := newSummary()
	go func() {
		defer close(summary.done)
		s, _ := run.Wait()
		if s == nil {
			return
		}
		summary.InputLines = s.InputLines
		summary.OutputLines = s.OutputLines
		summary.EventsFound = s.EventsFound
		summary.EventsEmitted = s.EventsEmitted
		summary.EventsDeduped = s.EventsDeduped
		summary.EventsDroppedBudget = s.EventsDroppedBudget
		summary.EventsTruncated = s.EventsTruncated
		summary.FramesCollapsed = s.FramesCollapsed
		summary.EstimatedTokens = s.EstimatedTokens
		summary.Estimator = s.Estimator
		summary.ExitCode = s.ExitCode
	}()
	return channel, summary, nil
}

// translateOptions converts the public Options into the
// orchestrator's internal Config vocabulary. Setup errors that
// can be detected synchronously (unknown Output, unknown Tokenizer
// for the empty case) surface here so the caller sees them before
// any goroutine starts.
func translateOptions(opts Options) (orchestrator.Config, error) {
	out, err := translateOutput(opts.Output)
	if err != nil {
		return orchestrator.Config{}, err
	}
	return orchestrator.Config{
		Format:        opts.Format,
		Strict:        opts.Strict,
		Output:        out,
		Budget:        opts.Budget,
		Tokenizer:     opts.Tokenizer,
		DedupeWindow:  opts.DedupeWindow,
		KeepVendor:    opts.KeepVendor,
		KeepWarnings:  opts.KeepWarnings,
		MinSeverity:   opts.MinSeverity,
		MaxEvents:     opts.MaxEvents,
		ContextLines:  opts.ContextLines,
		StripEnvelope: opts.StripEnvelope,
		Writer:        opts.Writer,
		NoFooter:      opts.NoFooter,
		FenceLang:     opts.FenceLang,
	}, nil
}

// translateOutput maps the public OutputFormat string onto the
// orchestrator's typed constant. The empty string maps to
// OutputText so the public Options zero value is a sensible
// default; every other unrecognised value returns ErrUnknownOutput.
func translateOutput(o OutputFormat) (orchestrator.Output, error) {
	switch o {
	case "", OutputText:
		return orchestrator.OutputText, nil
	case OutputJSON:
		return orchestrator.OutputJSON, nil
	case OutputJSONStreaming:
		return orchestrator.OutputJSONStreaming, nil
	case OutputMarkdown:
		return orchestrator.OutputMarkdown, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrUnknownOutput, string(o))
	}
}

// translateOrchestratorError maps the orchestrator's internal
// sentinel errors onto the package's public sentinel errors so
// library callers can use errors.Is against the public names
// without importing the internal package.
func translateOrchestratorError(err error) error {
	switch {
	case errors.Is(err, orchestrator.ErrNilWriter):
		return ErrNilWriter
	case errors.Is(err, orchestrator.ErrNilReader):
		// The library API doesn't expose a NilReader error; the
		// orchestrator's ErrNilReader maps to a generic message
		// because the public surface uses opts as the entry point.
		return fmt.Errorf("distill: nil io.Reader: %w", err)
	case errors.Is(err, orchestrator.ErrUnknownTokenizer):
		return fmt.Errorf("%w: %s", ErrUnknownTokenizer, sentinelMessage(err))
	case errors.Is(err, orchestrator.ErrUnknownFormat):
		return fmt.Errorf("%w: %s", ErrUnknownFormat, sentinelMessage(err))
	case errors.Is(err, orchestrator.ErrUnknownOutput):
		return fmt.Errorf("%w: %s", ErrUnknownOutput, sentinelMessage(err))
	case errors.Is(err, orchestrator.ErrUnknownStripEnvelope):
		return fmt.Errorf("%w: %s", ErrUnknownStripEnvelope, sentinelMessage(err))
	default:
		return fmt.Errorf("distill: %w", err)
	}
}

// sentinelMessage extracts the parametric part of an orchestrator
// sentinel error — the value name after the last colon. The
// orchestrator wraps its sentinels with `%w: %q`, so the message
// shape is "orchestrator: unknown Tokenizer: \"ggml\"". We strip
// every leading "<word>: " segment so the public error reads
// "distill: unknown Tokenizer value: \"ggml\"".
func sentinelMessage(err error) string {
	msg := err.Error()
	for {
		i := strings.Index(msg, ": ")
		if i < 0 {
			return msg
		}
		// Each "word: " prefix corresponds to a wrap layer. Strip
		// one wrap at a time until we land on a value that doesn't
		// look like a Go identifier-then-colon shape.
		head := msg[:i]
		if !isIdentifierLike(head) {
			return msg
		}
		msg = msg[i+2:]
	}
}

// isIdentifierLike reports whether s is a single lowercase-ish
// word likely to be a package name or sentinel-error noun. Used
// by sentinelMessage to decide when to stop stripping prefixes.
func isIdentifierLike(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '_' && r != ' ' {
			return false
		}
	}
	return true
}
