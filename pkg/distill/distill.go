// Package distill is the stable public library API for distill-ai.
//
// Most users invoke the CLI binary (`distill-ai`) directly. This
// package exists for code that wants to embed the distillation
// pipeline as a library: a custom tool, a server, a test runner.
//
// In milestone M1 (the current state) this package re-exports the
// core types only. The streaming entry point — `Distill(ctx, r, opts)
// (<-chan Event, error)` — lands in M14. Until then the exported type
// aliases let downstream code import this package without depending on
// internal/, so M14 doesn't have to restructure imports.
//
// See ARCHITECTURE.md § Library API for the design intent and
// CHANGELOG.md for the public API timeline.
package distill

import (
	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
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
