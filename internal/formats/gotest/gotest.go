// Package gotest implements the Format plugin for `go test` output.
// It is the first specific (non-generic) Format to ship and the
// format this very project emits on every `make test`, so it doubles
// as distill-ai's canonical dogfood loop.
//
// # Detection model
//
// The detector raises Confidence to 1.0 on any of these unambiguous
// markers in the 4 KiB sample:
//
//   - `^--- FAIL: ` — the per-test failure header the default
//     reporter emits.
//   - `^FAIL\t<pkg>` — the per-package summary line, where <pkg>
//     looks like an importable Go package path (`/`-separated or
//     `pkg.subpkg`-style identifiers). The package-token guard keeps
//     unrelated tools that print a bare `FAIL` from claiming the
//     format.
//   - `^=== RUN   ` — the verbose-mode test header.
//
// The detector raises Confidence to 0.8 on a goroutine-dump header
// (`^goroutine \d+ \[\w+\]:`) plus at least one Go file reference
// (`\.go:\d+`). This catches bare panics emitted by `go run` with no
// surrounding test scaffolding.
//
// Anything else returns 0.0.
//
// See docs/formats/gotest.md for the user-facing description of what
// the parser extracts and what it drops, and TODO.md § M10 for the
// milestone scope.
package gotest

import (
	"context"
	"io"
	"regexp"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// Confidence thresholds. Named constants so the design intent is
// reviewable without grepping the regex table.
const (
	// confidenceClearMarker is returned when the sample contains an
	// unambiguous gotest marker. 1.0 mirrors the convention used by
	// pytest (M11) and jest (M12).
	confidenceClearMarker event.Confidence = 1.0

	// confidenceFuzzy is returned when the sample contains a Go
	// goroutine dump but no test framing — likely a `go run` panic
	// without the surrounding `go test` scaffold. Specific enough
	// to win against generic but ambiguous enough that a more
	// specific format (e.g. a future panic-only format) could
	// reasonably outscore it.
	confidenceFuzzy event.Confidence = 0.8
)

// failHeaderPattern matches the per-test failure header gotest's
// default reporter emits. Anchored at start-of-line so prose
// containing the substring elsewhere doesn't false-positive.
var failHeaderPattern = regexp.MustCompile(`(?m)^--- FAIL: `)

// runHeaderPattern matches gotest's `-v` per-test header.
var runHeaderPattern = regexp.MustCompile(`(?m)^=== RUN {3}`)

// failPackagePattern matches gotest's per-package summary line
// (`FAIL\t<pkg>\t...`). The trailing token must look like a Go
// package path — at least one `/` (import-path form) or a
// dot-separated identifier sequence (`foo.bar.baz`). Bare `FAIL:
// rebooting node` from other tools therefore does not raise the
// score.
var failPackagePattern = regexp.MustCompile(`(?m)^FAIL\t(?:\S+/\S+|\w+(?:\.\w+)+)`)

// goroutineDumpPattern matches the head of a Go runtime goroutine
// dump. Used together with goFileRefPattern to detect bare panics
// without test scaffolding.
var goroutineDumpPattern = regexp.MustCompile(`(?m)^goroutine \d+ \[\w+\]:`)

// goFileRefPattern matches any `.go:NNN` reference. Cheap proxy for
// "this looks like Go output."
var goFileRefPattern = regexp.MustCompile(`\.go:\d+`)

// Format is the gotest parser. Implements formats.Format. Registered
// under the name "gotest" at init() time.
type Format struct{}

// Name returns "gotest" — the stable CLI identifier. Constant for
// the lifetime of the value.
func (Format) Name() string { return "gotest" }

// Detect reports Confidence for the sample per the rules documented
// on the package. See the package godoc for the marker catalogue
// and the rationale for each score.
func (Format) Detect(sample []byte) event.Confidence {
	if failHeaderPattern.Match(sample) {
		return confidenceClearMarker
	}
	if runHeaderPattern.Match(sample) {
		return confidenceClearMarker
	}
	if failPackagePattern.Match(sample) {
		return confidenceClearMarker
	}
	if goroutineDumpPattern.Match(sample) && goFileRefPattern.Match(sample) {
		return confidenceFuzzy
	}
	return 0
}

// Parse consumes r and forwards Events on the returned channel. The
// channel is closed exactly once when r reaches EOF, when ctx is
// cancelled, or when an unrecoverable I/O error occurs.
//
// M10.1 ships only the skeleton: Parse returns an immediately-closed
// channel with nil error so the M3 autodetection path can resolve
// the new format end-to-end before the real scanner arrives in
// M10.2.
func (Format) Parse(_ context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	out := make(chan event.Event)
	close(out)
	return out, nil
}

// init registers Format so the binary picks it up by side-effect
// import from cmd/distill-ai/register.go.
func init() { formats.Register(Format{}) }
