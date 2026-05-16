// Package event defines the core data model of distill-ai. Every format
// parser emits values of these types; every output encoder consumes
// them. The JSON shape is a stable public API — see
// docs/formats/SCHEMA.md.
package event

import (
	"fmt"
)

// Severity describes how bad an event is. Three levels keep the
// taxonomy small enough that formats can map onto it without ambiguity.
//
// Values are lowercase strings so they round-trip through JSON without
// translation.
type Severity string

const (
	// SeverityError indicates the input reported a failure: a failed test,
	// a panic, an exception, an HTTP 5xx.
	SeverityError Severity = "error"

	// SeverityWarn indicates a notable non-fatal event: a deprecation, a
	// skipped test, a retried timeout.
	SeverityWarn Severity = "warn"

	// SeverityInfo indicates a neutral notable event. Used sparingly;
	// most informational lines are dropped during distillation.
	SeverityInfo Severity = "info"
)

// String returns the wire representation of s. Total over the three
// defined constants; for any other value, returns the underlying string
// unchanged so unknown severities round-trip rather than vanish.
func (s Severity) String() string {
	return string(s)
}

// ParseSeverity converts a wire-format severity string to a Severity
// value. Returns an error for unknown strings so callers can distinguish
// "this format emitted an unknown severity" from "this severity is
// 'error'". Case-sensitive: matches lowercase only, per the schema.
func ParseSeverity(s string) (Severity, error) {
	switch Severity(s) {
	case SeverityError, SeverityWarn, SeverityInfo:
		return Severity(s), nil
	default:
		return "", fmt.Errorf("unknown severity %q", s)
	}
}

// Location identifies a source position attached to an Event. Optional
// on Event because not every parsed event has a known file:line (e.g.,
// a top-level panic with no traceback).
//
// Stored as a pointer in Event so the JSON output renders as `null`
// rather than an empty object when absent.
type Location struct {
	// File is the path as it appeared in the input. distill-ai does
	// not normalise or resolve it.
	File string `json:"file"`

	// Line is the 1-indexed line number.
	Line int `json:"line"`

	// Column is the 1-indexed column, when available. Optional;
	// renders as `null` when zero.
	Column *int `json:"column"`
}

// StackFrame is one frame of a structured stack trace. Formats that
// can't extract structured frames leave Event.Frames nil and put the
// raw trace text into Event.Body instead.
type StackFrame struct {
	// File is the frame's source file as printed in the trace.
	File string `json:"file"`

	// Line is the 1-indexed line number in File.
	Line int `json:"line"`

	// Function is the function or method name. Optional because some
	// trace formats omit it.
	Function string `json:"function,omitempty"`

	// Vendor is true if the frame was identified as third-party /
	// library / runtime code and therefore a candidate for collapsing
	// when --keep-vendor is false. See internal/event.collapse (M5).
	Vendor bool `json:"vendor"`
}

// Event is the unit of distillation: one parsed, structured occurrence
// extracted from the input stream. Parsers emit Events; the pipeline
// passes them through dedupe, frame collapse, and budget enforcement;
// the output encoder serialises them.
//
// The JSON shape (tags below) is a public API. See
// docs/formats/SCHEMA.md for the contract; do not change tags without
// bumping schema_version per AGENTS.md output stability.
type Event struct {
	// Severity classifies the event. Required.
	Severity Severity `json:"severity"`

	// Kind is the format-specific event type, e.g. "test_failure",
	// "panic", "snapshot_mismatch". Lowercase snake_case. Required.
	// See docs/formats/SCHEMA.md § Kind values for per-format lists.
	Kind string `json:"kind"`

	// Title is a one-line human-readable summary of the event,
	// extracted from the input. Required.
	Title string `json:"title"`

	// Location is the source position the event refers to, when
	// known. Renders as `null` when nil.
	Location *Location `json:"location"`

	// Body is the relevant verbatim lines from the input. Required;
	// always non-nil even if empty.
	Body []string `json:"body"`

	// Context is the surrounding lines (count controlled by
	// --context=N). Optional; omitted from JSON when empty.
	Context []string `json:"context,omitempty"`

	// Frames is the structured stack trace, when extractable.
	// Optional; omitted from JSON when empty.
	Frames []StackFrame `json:"frames,omitempty"`

	// FramesCollapsed is the number of vendor frames omitted from
	// Frames during collapse. Zero when no collapse occurred or when
	// --keep-vendor was set.
	FramesCollapsed int `json:"frames_collapsed"`

	// Count is the dedupe count: 1 for unique events, >1 when this
	// event represents a collapsed series of identical events.
	Count int `json:"count"`

	// Truncated is true if --budget enforcement forced the body to be
	// truncated.
	Truncated bool `json:"truncated"`

	// Metadata holds format-specific extra fields as a flat
	// string-to-string map. Optional; omitted from JSON when empty.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Raw is the original input bytes that produced this event, used
	// by the --passthrough fallback. Internal-only; never marshalled
	// to JSON.
	Raw string `json:"-"`
}

// Confidence is a format detector's self-reported certainty that a
// given input sample is in its format. Range: 0.0 (definitely not) to
// 1.0 (definitely yes). The detector in internal/detect picks the
// highest-confidence format ≥ ConfidenceMinDetect.
type Confidence float64

// ConfidenceMinDetect is the threshold below which the detector
// declines to choose a specific format and falls back to "generic" (or
// returns an error under --strict).
const ConfidenceMinDetect Confidence = 0.6
