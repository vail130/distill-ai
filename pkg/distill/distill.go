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
