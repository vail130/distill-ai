package distill

import (
	"io"

	"github.com/vail130/distill-ai/internal/event"
)

// OutputFormat selects which encoder Distill installs as the
// pipeline's Sink. Each value maps to one of the encoders in
// internal/output. Library callers pass one of the OutputText /
// OutputJSON / OutputJSONStreaming / OutputMarkdown constants on
// Options; an empty value is treated as OutputText (the documented
// default).
//
// The type is a named string rather than a re-export of the internal
// Sink type so library callers don't drag in internal/output as a
// transitive dependency. Distill translates the constant into the
// concrete Sink internally.
type OutputFormat string

// OutputFormat constants enumerate the four shipped encoders.
const (
	// OutputText is the compact human-readable encoder (text/plain).
	// Matches `distill-ai run --output=text`. Default when the
	// Options.Output field is the zero value.
	OutputText OutputFormat = "text"

	// OutputJSON is the schema-versioned batch JSON encoder. Matches
	// `distill-ai run --output=json` without --output-streaming. The
	// encoder buffers the full Event stream and emits a single
	// top-level object on EOF; streaming consumers should prefer
	// OutputJSONStreaming.
	OutputJSON OutputFormat = "json"

	// OutputJSONStreaming is the ndjson encoder. Each Event emits
	// as a self-contained JSON object on its own line; the final
	// line is the summary trailer. Matches `distill-ai run
	// --output=json --output-streaming`. Use this when the consumer
	// reads events as they arrive (a long-running tail, an MCP
	// server, an agent's stdin pipe).
	OutputJSONStreaming OutputFormat = "json-streaming"

	// OutputMarkdown is the chat-paste-friendly encoder. Matches
	// `distill-ai run --output=markdown`. Each Event renders as a
	// markdown heading plus fenced body block.
	OutputMarkdown OutputFormat = "markdown"
)

// String implements fmt.Stringer. Returns the constant name (the
// same string OutputFormat values compare against) so diagnostic
// messages and tests don't have to type-assert.
func (o OutputFormat) String() string {
	if o == "" {
		return string(OutputText)
	}
	return string(o)
}

// Options bundles every knob Distill accepts. The zero value is
// safe in every field except Writer: a Distill call with a nil
// Writer returns ErrNilWriter without starting the pipeline.
//
// Options is semantically aligned with internal/pipeline.Options but
// is a distinct type by design — pipeline.Options exposes
// implementation detail (the Stage chain, the buffer-size knob, the
// fan-in channel for envelope signals) that library callers should
// not depend on. Distill translates Options into pipeline.Options
// internally; future internal restructuring won't break callers.
//
// Config-file loading is deliberately not part of the library API.
// Library callers compose their own Options; internal/config is not
// exposed. A library that wants TOML config-file support imports
// internal/config via its own intermediate package, or waits for a
// v1.x decision to promote it. See docs/library-api.md for the
// rationale.
type Options struct {
	// Format is the explicit format name (e.g., "gotest", "pytest").
	// Empty means autodetect: Distill calls detect.Detect on a
	// sample of the input and uses the highest-confidence Format.
	// Equivalent to the CLI's positional FORMAT argument.
	Format string

	// Strict, when true, makes detection fail with an error instead
	// of falling back to the generic Format when no specific Format
	// scores above the detection threshold (ConfidenceMinDetect).
	// Equivalent to the CLI's --strict flag. Ignored when Format is
	// non-empty (an explicit format bypasses detection entirely).
	Strict bool

	// Output selects the Sink. Empty maps to OutputText (the default).
	// Distill returns ErrUnknownOutput for an unrecognised value.
	Output OutputFormat

	// Budget caps the estimated total token cost of the emitted
	// Event stream. Zero (the default) disables the cap. When
	// non-zero, BudgetStage joins the pipeline and may drop or
	// truncate lower-severity Events to fit; the Summary's
	// EventsDroppedBudget and EventsTruncated fields report what
	// happened. Equivalent to the CLI's --budget flag.
	Budget int

	// Tokenizer names the token estimator the BudgetStage uses.
	// Valid values: "heuristic" (the zero-dep word/symbol count
	// estimator; the default), "tiktoken" (the OpenAI BPE
	// tokenizer; exact for GPT-4/Claude). Empty maps to
	// "heuristic". An unknown value returns ErrUnknownTokenizer
	// before any pipeline goroutine starts.
	Tokenizer string

	// DedupeWindow is the LRU capacity used by DedupeStage. Zero or
	// negative disables dedupe (every Event flows through with
	// Count=1). Positive enables dedupe with the specified
	// capacity. Equivalent to the CLI's --dedupe-window flag.
	DedupeWindow int

	// KeepVendor, when true, preserves vendor / stdlib stack frames
	// instead of collapsing them. The Events still carry their
	// Frames; the CollapseStage's vendor classification still runs
	// (so encoders can style vendor frames distinctly) but no
	// frames are removed. Equivalent to the CLI's --keep-vendor
	// flag.
	KeepVendor bool

	// KeepWarnings, when true, drops the effective minimum severity
	// to event.SeverityWarn regardless of MinSeverity. Per-format
	// opt-in; honoured by the generic format and any specific
	// format that opts in. Equivalent to the CLI's --keep-warnings
	// flag.
	KeepWarnings bool

	// MinSeverity is the lowest severity a parser should emit.
	// Empty (the zero value of event.Severity, which equals "")
	// defers to the format's default; the generic format treats
	// empty as event.SeverityError. KeepWarnings can lower the
	// effective floor. Equivalent to the CLI's --severity flag.
	MinSeverity event.Severity

	// MaxEvents caps the total number of Events emitted to the
	// Sink. Zero (the default) disables the cap. When non-zero,
	// MaxEventsStage joins the pipeline after BudgetStage so the
	// budget enforcer's severity-priority truncation runs first
	// and the cap then trims to the top N. Equivalent to the CLI's
	// --max-events flag.
	MaxEvents int

	// ContextLines is the number of source-line-context entries
	// each Event should carry. Zero defers to the format's
	// default (3 for the generic format). Equivalent to the CLI's
	// --context flag.
	ContextLines int

	// StripEnvelope selects the envelope stripper that runs before
	// format detection. Valid values: "auto" (run envelope
	// detection; the default), "none" (skip the envelope step),
	// or the name of a registered stripper (e.g., "github-actions",
	// "gitlab-ci"). An empty string is treated as "auto".
	// Equivalent to the CLI's --strip-envelope flag.
	StripEnvelope string

	// Writer is the destination for the chosen Sink's output. A nil
	// Writer returns ErrNilWriter from Distill without starting the
	// pipeline. Required.
	Writer io.Writer

	// NoFooter suppresses the human-readable footer block on the
	// text and markdown Sinks. No-op for the JSON Sinks, where the
	// summary is part of the schema and is always emitted.
	// Equivalent to the CLI's --no-footer flag.
	NoFooter bool

	// FenceLang is the language label for the fenced code blocks
	// the markdown Sink emits (e.g., "go", "python"). Ignored when
	// Output is not OutputMarkdown. Empty means no language label
	// (a bare ``` fence).
	FenceLang string
}

// Summary captures the run-level counters and exit-code-relevant
// signals Distill populates after the Event channel closes. The
// fields are a re-export of the JSON-schema summary object
// documented in docs/formats/SCHEMA.md § Summary object; a library
// caller and a JSON consumer of `distill-ai run --output=json`
// observe the same numbers.
//
// Fields are valid only after the Event channel returned by Distill
// closes. Reading them while events are still flowing returns
// in-flight (possibly zero) values; Distill documents this timing
// contract.
type Summary struct {
	// InputLines is the total lines consumed from the input
	// io.Reader. Reflects the line-count footer in
	// `distill-ai run --output=text`.
	InputLines int

	// OutputLines is the total lines written to the Writer.
	OutputLines int

	// EventsFound is the number of Events detected by the parser
	// before any downstream filtering or budget enforcement.
	EventsFound int

	// EventsEmitted is the number of Events actually written to
	// the Writer. The library caller maps this to exit code 1
	// (no events) via ExitCodeFromSummary.
	EventsEmitted int

	// EventsDeduped is the count of Events collapsed into a
	// `count > 1` Event by DedupeStage. Reported as the total
	// duplicates removed, not the number of surviving Events with
	// count > 1.
	EventsDeduped int

	// EventsDroppedBudget is the number of Events the BudgetStage
	// dropped entirely to fit within Options.Budget. Non-zero
	// values map to exit code 3 via ExitCodeFromSummary.
	EventsDroppedBudget int

	// EventsTruncated is the number of Events whose body was
	// shortened by the BudgetStage to fit within Options.Budget.
	// The Event is still emitted; only its Body shrinks. Non-zero
	// values map to exit code 3 via ExitCodeFromSummary.
	EventsTruncated int

	// FramesCollapsed is the total vendor frames removed across
	// every Event by CollapseStage.
	FramesCollapsed int

	// EstimatedTokens is the estimated token cost of the emitted
	// Event stream, as computed by the BudgetStage's Estimator.
	// Zero when Options.Budget is zero (no BudgetStage runs).
	EstimatedTokens int

	// Estimator is the name of the estimator that produced
	// EstimatedTokens — "heuristic" or "tiktoken". Empty when
	// Options.Budget is zero.
	Estimator string

	// ExitCode is the canonical exit code for the run: 0 (clean),
	// 1 (no events emitted), 3 (BudgetStage forced drops or
	// truncations). Setup errors return non-nil from Distill
	// instead of populating this field; ExitCode = 2 is reserved
	// for that case and is not set by Distill itself.
	ExitCode int
}

// ForcedDrops reports whether the BudgetStage either dropped or
// truncated Events under Options.Budget. Mirrors
// internal/pipeline.BudgetCounters.ForcedDrops so library callers
// don't have to inspect the Summary's individual counters to
// answer "did the budget force a partial run?".
//
// Safe on a nil receiver: returns false.
func (s *Summary) ForcedDrops() bool {
	if s == nil {
		return false
	}
	return s.EventsDroppedBudget > 0 || s.EventsTruncated > 0
}
