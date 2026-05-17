// Package formats defines the Format plugin contract that every
// input-parsing implementation in distill-ai must satisfy, plus the
// thread-safe registry that lets formats self-register at init time.
//
// See ARCHITECTURE.md § Format plugin contract for the design,
// CONTRIBUTING.md § Adding a format for the contributor workflow, and
// the godoc on Format below for the per-method spec.
package formats

import (
	"context"
	"io"

	"github.com/vail130/distill-ai/internal/event"
)

// Format is the plugin contract a parser must satisfy to participate in
// distill-ai's pipeline. Each supported input format (pytest, jest, go
// test, kubernetes logs, ...) is an implementation of this interface,
// registered via Register from an init() function in its own package so
// the binary picks it up by import.
//
// Implementations must be safe for concurrent use: the pipeline may
// call Detect and Parse concurrently across different inputs, and the
// registry exposes Format values to multiple goroutines.
//
// A reference implementation is available in
// internal/formats/example_test.go; the contributor workflow is in
// CONTRIBUTING.md § Adding a format.
type Format interface {
	// Name returns the stable, lowercase identifier used on the CLI
	// (e.g. "pytest", "jest", "gotest"). Must be unique across all
	// registered formats — duplicate Register calls panic at init
	// time. Returned value must be constant for the lifetime of the
	// Format value.
	Name() string

	// Detect inspects an opening sample of input (typically the first
	// 4KB, see internal/detect.SampleSize) and returns a self-reported
	// confidence in [0.0, 1.0] that the input is in this format.
	//
	// Implementations may not modify the sample slice and may not
	// retain it beyond the call. Implementations must be cheap:
	// detection runs against every registered format on every input,
	// so anything beyond a regex match or a few field probes is
	// suspect.
	//
	// Sample may be shorter than 4KB on small inputs; implementations
	// must handle empty and truncated samples without panicking.
	Detect(sample []byte) event.Confidence

	// Parse consumes r and emits Events on the returned channel.
	//
	// Lifecycle and contract:
	//
	//   - Parse must close the channel exactly once when r reaches EOF,
	//     when ctx is cancelled, or when an unrecoverable parse error
	//     occurs. After close, callers may continue to drain in-flight
	//     events from the channel.
	//   - Parse must not block indefinitely. If ctx is cancelled,
	//     Parse must close the channel and return promptly.
	//   - If Parse encounters a non-fatal parsing problem (e.g., a
	//     malformed event embedded in otherwise-valid input), it
	//     should emit a best-effort Event and continue rather than
	//     returning an error. Reserve error returns for I/O failures
	//     and context cancellation.
	//   - The returned error, if any, is what Parse encountered before
	//     it started or during streaming; the channel may still have
	//     emitted partial output before the error. Callers must drain
	//     the channel before inspecting the error.
	//   - Implementations may not retain r after Parse returns.
	//
	// opts carries per-invocation parsing tweaks (context lines,
	// vendor-frame policy, etc.). See ParseOpts.
	Parse(ctx context.Context, r io.Reader, opts ParseOpts) (<-chan event.Event, error)
}

// ParseOpts carries the parsing options the pipeline passes to every
// Format.Parse call. Fields are added as later milestones need them;
// the zero value of every field must be a sensible default so callers
// can pass ParseOpts{} during development.
type ParseOpts struct {
	// ContextLines is the number of source-line-context entries each
	// parsed Event should attempt to carry. Zero means no context;
	// negative values are treated as zero. Parsers may emit fewer
	// lines than requested if the input does not provide them.
	ContextLines int

	// KeepVendor, when true, instructs the parser to leave vendor /
	// library / runtime stack frames in Event.Frames rather than
	// flagging them for collapse downstream. Frame classification
	// happens in internal/event.collapse (M5); parsers only set
	// StackFrame.Vendor.
	KeepVendor bool

	// MinSeverity is the lowest severity a parser should emit. The
	// zero value (empty Severity) means "format-default", which the
	// generic format treats as event.SeverityError. KeepWarnings
	// overrides this — see below.
	//
	// Per-format opt-in: not every Format honours this field. The
	// generic format (M9.4) does; specific formats (gotest, pytest,
	// jest) wire it in when their milestones land. SCHEMA.md
	// documents per-format opt-in so consumers don't expect a
	// pipeline-wide guarantee.
	MinSeverity event.Severity

	// KeepWarnings, when true, drops the effective minimum severity
	// to warn regardless of MinSeverity. Matches ARCHITECTURE.md's
	// `--keep-warnings` flag: a one-shot bump for the common
	// "errors only when errors exist, otherwise everything" case.
	//
	// Precedence: if MinSeverity is explicitly set to SeverityInfo,
	// the parser still emits warnings (the explicit MinSeverity
	// wins over KeepWarnings=false).
	KeepWarnings bool
}
