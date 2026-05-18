// Package jest implements the Format plugin for jest output. It is
// the third specific (non-generic) Format to ship, after gotest and
// pytest. Jest covers the JavaScript/TypeScript test-runner niche
// that pytest fills for Python and gotest fills for Go.
//
// # Detection model
//
// The detector raises Confidence to 1.0 on any of these unambiguous
// markers in the 4 KiB sample:
//
//   - `^\s*● ` — the bullet jest's default reporter prints at the
//     head of every failure block. The bullet character is U+25CF
//     BLACK CIRCLE, which no other test runner in the v1 catalogue
//     uses; high specificity for very little detection cost.
//   - `^(FAIL|PASS) ` followed by a path-shaped token. The path
//     guard mirrors gotest's package-token guard from M10.1:
//     unrelated tools printing a bare `FAIL` line do not raise the
//     score. The token is recognised when it contains a path
//     separator (`/` or `\`) or ends in one of the test-file
//     extensions jest discovers by default (`.test.{js,ts,jsx,tsx}`,
//     `.spec.{js,ts,jsx,tsx}`).
//
// The detector raises Confidence to 0.8 on the combined signal of
// a `Tests:` summary line (`^Tests:\s+\d+ (passed|failed|skipped|total)`)
// **and** a mention of either `jest` or a `.test.`/`.spec.` filename
// elsewhere in the sample. The combined requirement keeps the fuzzy
// match from claiming arbitrary log output that happens to contain
// a "Tests:" word.
//
// Anything else returns 0.0.
//
// M12.1 ships detect + skeleton only. Parse returns an
// immediately-closed channel; the real scanner lands in M12.2
// (failure blocks), M12.3 (snapshot mismatches), and M12.4 (stack
// frames, suite errors, and the verbose / CI reporter modes).
// See docs/formats/jest.md for the user-facing description and
// TODO.md § M12 for the milestone scope.
package jest

import (
	"context"
	"io"
	"regexp"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// Confidence thresholds. Named constants so the design intent is
// reviewable without grepping the regex table. Mirrors the
// gotest (M10) and pytest (M11) convention.
const (
	// confidenceClearMarker is returned when the sample contains an
	// unambiguous jest marker.
	confidenceClearMarker event.Confidence = 1.0

	// confidenceFuzzy is returned when the sample carries jest-
	// shaped indicators (a Tests: summary plus a jest / .test. /
	// .spec. corroborator) without a clear marker. Common when the
	// caller pipes a truncated tail of a long run.
	confidenceFuzzy event.Confidence = 0.8
)

// bulletFailurePattern matches the `●` failure-block header jest's
// default reporter emits. The character is U+25CF BLACK CIRCLE,
// distinctive enough that no v1-catalogue runner false-positives on
// it. Anchored at start-of-line (after optional leading whitespace —
// jest indents the bullet two spaces in some reporter configs).
var bulletFailurePattern = regexp.MustCompile(`(?m)^\s*● `)

// failPassHeaderPattern matches the per-file header jest emits
// before each test file's results: `FAIL src/auth.test.js`,
// `PASS src/utils.spec.ts`, and so on. The token after the
// `FAIL|PASS` must look like a test-file path — see
// pathTokenPattern below. The package-token-guard approach is the
// same one M10.1 uses for `FAIL\t<pkg>`.
var failPassHeaderPattern = regexp.MustCompile(`(?m)^(FAIL|PASS) (\S+)`)

// pathTokenPattern recognises path tokens for the FAIL/PASS-header
// guard. A token counts as a path when it contains a `/` or `\`, or
// when it ends in one of jest's default test-file suffixes.
var pathTokenPattern = regexp.MustCompile(`(?:[/\\]|\.(?:test|spec)\.(?:js|ts|jsx|tsx)$)`)

// testsSummaryPattern matches jest's terminal summary line:
// `Tests:       1 failed, 2 passed, 3 total`. The leading whitespace
// after the colon is variable across reporter configs; the regex
// matches at least one space and one digit so that prose containing
// the literal word "Tests:" alone does not match.
var testsSummaryPattern = regexp.MustCompile(`(?m)^Tests:\s+\d+\s+(?:passed|failed|skipped|total)`)

// jestCorroboratorPattern matches a corroborating signal that the
// summary belongs to a jest run rather than some other Tests:
// summary shape: either a literal `jest` word or a path that ends
// in a jest-default test-file extension.
var jestCorroboratorPattern = regexp.MustCompile(`\bjest\b|\.(?:test|spec)\.(?:js|ts|jsx|tsx)\b`)

// Format is the jest parser. Implements formats.Format. Registered
// under the name "jest" at init() time.
type Format struct{}

// Name returns "jest" — the stable CLI identifier. Constant for
// the lifetime of the value.
func (Format) Name() string { return "jest" }

// Detect reports Confidence for the sample per the rules documented
// on the package. See the package godoc for the marker catalogue
// and the rationale for each score.
func (Format) Detect(sample []byte) event.Confidence {
	if bulletFailurePattern.Match(sample) {
		return confidenceClearMarker
	}
	if m := failPassHeaderPattern.FindSubmatch(sample); m != nil {
		// m[2] is the token after FAIL/PASS. The path guard
		// rejects bare `FAIL: rebooting` style output from
		// unrelated tools.
		if pathTokenPattern.Match(m[2]) {
			return confidenceClearMarker
		}
	}
	if testsSummaryPattern.Match(sample) && jestCorroboratorPattern.Match(sample) {
		return confidenceFuzzy
	}
	return 0
}

// Parse consumes r and forwards Events on the returned channel. The
// channel is closed exactly once when r reaches EOF, when ctx is
// cancelled, or when an unrecoverable I/O error occurs.
//
// M12.2 ships the `●` block scanner that emits one Event per
// failure block with `Severity=error` and `Kind=test_failure`.
// M12.3 will distinguish snapshot mismatches; M12.4 adds
// stack-frame extraction and `suite_error` kinds plus the
// `--verbose` and CI reporter modes.
func (Format) Parse(ctx context.Context, r io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	return parseStream(ctx, r), nil
}

// init registers Format so the binary picks it up by side-effect
// import from cmd/distill-ai/register.go.
func init() { formats.Register(Format{}) }
