// Package gotestsum implements the Format plugin for gotestsum-style
// Go test summaries.
//
// The parser covers the human-readable summary format emitted by
// gotest.tools/gotestsum and similar wrappers around `go test -json`:
// status lines (`FAIL pkg.Test`), an `=== Failed` section, `=== FAIL:`
// blocks, and a trailing `DONE ...` summary.
package gotestsum

import (
	"context"
	"io"
	"regexp"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

const confidenceClearMarker event.Confidence = 1.0

var (
	failedSectionPattern = regexp.MustCompile(`(?m)^=== Failed$`)
	failHeaderPattern    = regexp.MustCompile(`(?m)^=== FAIL: `)
	doneSummaryPattern   = regexp.MustCompile(`(?m)^DONE \d+ tests?`)
	statusLinePattern    = regexp.MustCompile(`(?m)^(?:PASS|FAIL|SKIP) \S+\.\S+ \([^)]+\)$`)
)

// Format is the gotestsum parser. It is registered under the stable
// CLI identifier "gotestsum".
type Format struct{}

// Name returns the stable CLI identifier for gotestsum input.
func (Format) Name() string { return "gotestsum" }

// Detect reports Confidence for gotestsum summary markers.
func (Format) Detect(sample []byte) event.Confidence {
	if failedSectionPattern.Match(sample) {
		return confidenceClearMarker
	}
	if failHeaderPattern.Match(sample) {
		return confidenceClearMarker
	}
	if doneSummaryPattern.Match(sample) {
		return confidenceClearMarker
	}
	if statusLinePattern.Match(sample) {
		return confidenceClearMarker
	}
	return 0
}

// Parse consumes gotestsum output and emits one Event per concrete
// failed package/test block. A failing DONE summary emits a fallback
// Event only when no concrete block was present.
func (Format) Parse(ctx context.Context, r io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	return parseStream(ctx, r), nil
}

func init() { formats.Register(Format{}) }
