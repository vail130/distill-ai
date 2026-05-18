// Package pytest implements the Format plugin for pytest output.
// It is the second specific (non-generic) Format to ship, after
// gotest. Pytest is the most-used non-Go test runner in the
// agent-debugging ecosystem; the project itself does not emit
// pytest output, so the format also serves as the cross-check that
// the shared format-test harness in internal/formats generalises
// beyond gotest's shape.
//
// # Detection model
//
// The detector raises Confidence to 1.0 on any of these unambiguous
// markers in the 4 KiB sample:
//
//   - `=== test session starts ===` — the canonical session header
//     pytest prints at the top of every run.
//   - `=== FAILURES ===` — the per-failure summary section. The
//     parser will scan blocks beneath this banner in M11.2.
//
// The detector raises Confidence to 0.8 on a `>` assertion line
// (the `>   assert ...` indicator pytest uses inside long-form
// tracebacks) together with a mention of `conftest.py` or
// `pytest.ini`. The combined requirement keeps the fuzzy match
// from claiming arbitrary diff output that happens to use `>`.
//
// Anything else returns 0.0.
//
// M11.1 ships detect + skeleton only. Parse returns an
// immediately-closed channel; the real scanner lands in M11.2-M11.4.
// See docs/formats/pytest.md for the user-facing description and
// TODO.md § M11 for the milestone scope.
package pytest

import (
	"context"
	"io"
	"regexp"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// Confidence thresholds. Named constants so the design intent is
// reviewable without grepping the regex table. Mirrors the gotest
// convention from M10.
const (
	// confidenceClearMarker is returned when the sample contains an
	// unambiguous pytest banner.
	confidenceClearMarker event.Confidence = 1.0

	// confidenceFuzzy is returned when the sample carries pytest-
	// shaped indicators (assertion marker plus a config-file
	// reference) without a top-level banner. Common when the
	// caller pipes a truncated tail of a long run.
	confidenceFuzzy event.Confidence = 0.8
)

// sessionStartPattern matches the canonical pytest session header.
// Anchored at start-of-line so banners embedded in prose don't
// false-positive. The `=` run width varies slightly across pytest
// versions; `=+` matches them all.
var sessionStartPattern = regexp.MustCompile(`(?m)^=+ test session starts =+\s*$`)

// failuresBannerPattern matches the `=== FAILURES ===` banner
// pytest prints above per-failure long-form tracebacks.
var failuresBannerPattern = regexp.MustCompile(`(?m)^=+ FAILURES =+\s*$`)

// assertionMarkerPattern matches the `>   assert ...` indicator
// pytest inserts before the failing line inside a long-form
// traceback. Useful as a fuzzy signal because pytest is the only
// runner that uses this exact convention.
var assertionMarkerPattern = regexp.MustCompile(`(?m)^>\s+`)

// configFilePattern matches mentions of pytest's canonical config
// filenames. Combined with assertionMarkerPattern to disambiguate
// from arbitrary patch / quoted-output content that also starts a
// line with `>`.
var configFilePattern = regexp.MustCompile(`\b(?:conftest\.py|pytest\.ini)\b`)

// Format is the pytest parser. Implements formats.Format. Registered
// under the name "pytest" at init() time.
type Format struct{}

// Name returns "pytest" — the stable CLI identifier. Constant for
// the lifetime of the value.
func (Format) Name() string { return "pytest" }

// Detect reports Confidence for the sample per the rules documented
// on the package. See the package godoc for the marker catalogue
// and the rationale for each score.
func (Format) Detect(sample []byte) event.Confidence {
	if sessionStartPattern.Match(sample) {
		return confidenceClearMarker
	}
	if failuresBannerPattern.Match(sample) {
		return confidenceClearMarker
	}
	if assertionMarkerPattern.Match(sample) && configFilePattern.Match(sample) {
		return confidenceFuzzy
	}
	return 0
}

// Parse consumes r and forwards Events on the returned channel. The
// channel is closed exactly once when r reaches EOF, when ctx is
// cancelled, or when an unrecoverable I/O error occurs.
//
// M11.2 ships the `=== FAILURES ===` block scanner that emits one
// Event per failure with `Severity=error` and `Kind=test_failure`.
// M11.3 will add `=== ERRORS ===` and collection-error handling;
// M11.4 will add stack frame extraction and `--tb` shape
// detection.
func (Format) Parse(ctx context.Context, r io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	return parseStream(ctx, r), nil
}

// init registers Format so the binary picks it up by side-effect
// import from cmd/distill-ai/register.go.
func init() { formats.Register(Format{}) }
