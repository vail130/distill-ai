package gotest

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
)

// maxPanicLines caps how many lines a panic block may accumulate
// before the scanner truncates it. Larger than the M9 generic cap
// (100) because Go panics with chained goroutines often span 150+
// lines in real code. When the cap fires the final Body line
// becomes the documented sentinel and metadata.panic_truncated is
// set so encoders can render the case.
const maxPanicLines = 200

// panicTruncatedSentinel is the marker appended as the last Body
// line when maxPanicLines is hit. Tests pin the exact string.
const panicTruncatedSentinel = "... [panic block truncated]"

// Pre-compiled regexes for M10.3.
var (
	// panicHeaderPattern matches the leading `panic:` line. Anchored
	// at start-of-line because gotest never indents a top-level
	// panic header.
	panicHeaderPattern = regexp.MustCompile(`^panic: `)

	// panicContinuationPattern reports whether a line belongs to
	// an in-flight panic block. The shapes covered:
	//
	//   - `^\s` — indented stack tail lines (`\tpath:line +0xNN`),
	//     function-arg lines.
	//   - `^$` — blank lines inside the dump.
	//   - `^goroutine \d+ ` — goroutine headers.
	//   - `^panic: ` — chained panics from goroutines.
	//   - `^\[` — signal subheaders like `[signal SIGSEGV: ...]`,
	//     `[recovered]`.
	//   - `^created by ` — the per-goroutine creator line that
	//     follows the last frame in each goroutine.
	//   - `^[\w./*()]+\(.*\)$` — Go function-call lines. The args
	//     after the open paren accept any character: real panic
	//     output uses pointers, hex literals, struct literals
	//     (`{0x1, 0x2}`), interface types, etc. Anything between
	//     the matching outermost parens belongs to the call.
	panicContinuationPattern = regexp.MustCompile(
		`^(?:\s|$|goroutine \d+ |panic: |\[|created by |[\w./*()]+\(.*\)$)`,
	)

	// buildErrorLinePattern matches a Go compiler / vet error line:
	// `path/to/file.go:line:col: message`. The path token must end
	// in `.go` so we don't mismatch host:port pairs or other
	// `:N:N:` shapes. Anchored at start-of-line.
	buildErrorLinePattern = regexp.MustCompile(`^(\S+\.go):(\d+):(\d+):\s+(.*)$`)

	// buildFailureSummaryPattern matches `FAIL\t<pkg> [build
	// failed]` and `FAIL\t<pkg> [setup failed]` — the gotest tail
	// when tests didn't run because of compilation errors.
	buildFailureSummaryPattern = regexp.MustCompile(`^FAIL\t(\S+) \[(?:build|setup) failed\]$`)
)

// pendingPanic is the in-flight Event for a `panic:` block. The
// scanner accumulates body lines until the block terminates; at
// emit time the fields are projected into event.Event with
// Kind="panic".
type pendingPanic struct {
	header    string
	body      []string
	testID    string // most-recent --- FAIL: TestName; "" outside a fail block
	truncated bool
}

// finalisePanic builds the final event.Event for a panic block.
// Title is the trimmed `panic:` header. Body retains the verbatim
// dump. Frames are extracted via the goroutine-frame pair walker
// in frames.go; Location is the first user-code frame.
func finalisePanic(p *pendingPanic) event.Event {
	meta := map[string]string{}
	if p.testID != "" {
		meta["test_id"] = p.testID
	}
	if p.truncated {
		meta["panic_truncated"] = "true"
	}
	if len(meta) == 0 {
		meta = nil
	}
	frames := extractGoFrames(p.body)
	var loc *event.Location
	for _, fr := range frames {
		// First user-code frame: skip Go runtime / testing /
		// pkg.mod entries so the Location points at the user
		// frame rather than a runtime helper. The CollapseStage
		// also classifies these as vendor; M10.4 leans on the
		// classifier so the rule stays in one place.
		if !isLikelyVendor(fr.File, fr.Function) {
			f := fr
			loc = &event.Location{File: f.File, Line: f.Line}
			break
		}
	}
	return event.Event{
		Severity: event.SeverityError,
		Kind:     "panic",
		Title:    strings.TrimSpace(p.header),
		Location: loc,
		Body:     append([]string(nil), p.body...),
		Frames:   frames,
		Metadata: meta,
	}
}

// isLikelyVendor is a small heuristic for picking the first
// user-code frame when populating an Event's Location. Mirrors the
// patterns the M5 CollapseStage uses for Go: `/src/runtime/`,
// `pkg/mod/`, `/vendor/`, plus the `testing.tRunner` family that
// always wraps user tests.
//
// We don't reuse the M5 ClassifyFrames here because Location only
// needs the first user frame and false negatives are cheap
// (Location stays nil); the runtime-classifier work happens later
// when the Event passes through the CollapseStage.
func isLikelyVendor(file, fn string) bool {
	if strings.Contains(file, "/src/runtime/") ||
		strings.Contains(file, "/src/testing/") ||
		strings.Contains(file, "pkg/mod/") ||
		strings.Contains(file, "/vendor/") {
		return true
	}
	if strings.HasPrefix(fn, "runtime.") || strings.HasPrefix(fn, "testing.") {
		return true
	}
	return false
}

// pendingRace is the in-flight Event for a race-detector report.
// The scanner enters this state on the first `==================`
// divider and exits on the second. Body retains the verbatim block
// including dividers; Frames are extracted across both goroutine
// stacks the report contains.
type pendingRace struct {
	body      []string
	testID    string // most-recently-running test, when known
	truncated bool
}

// finaliseRace builds the final event.Event for a race-detector
// report. Title is the canonical `WARNING: DATA RACE` line; Body
// keeps the report verbatim. Frames are extracted from the goroutine
// stacks the report contains; metadata.race_goroutines counts how
// many stacks were merged (always 2 for the canonical report shape).
func finaliseRace(p *pendingRace) event.Event {
	frames := extractGoFrames(p.body)
	meta := map[string]string{}
	if p.testID != "" {
		meta["test_id"] = p.testID
	}
	meta["race_goroutines"] = "2"
	if p.truncated {
		meta["race_truncated"] = "true"
	}
	return event.Event{
		Severity: event.SeverityError,
		Kind:     "race_condition",
		Title:    raceConditionTitle,
		Body:     append([]string(nil), p.body...),
		Frames:   frames,
		Metadata: meta,
	}
}

// pendingBuild is the in-flight Event for a build error line. Build
// errors are typically one or two lines; the scanner emits one
// Event per matched `.go:line:col: msg` line. Multi-line errors
// (continuation arrows under the line, secondary context) are not
// modelled separately in v1 — they fall into the surrounding
// non-fail-block stream and are dropped, matching what the
// distilled output should look like.
type pendingBuild struct {
	file string
	line int
	col  int
	msg  string
	pkg  string // populated from FAIL\t<pkg> [build failed] when seen
}

// finaliseBuild builds the final event.Event for a build error.
func finaliseBuild(p *pendingBuild) event.Event {
	col := p.col
	loc := &event.Location{File: p.file, Line: p.line}
	if col > 0 {
		c := col
		loc.Column = &c
	}
	meta := map[string]string{}
	if p.pkg != "" {
		meta["package"] = p.pkg
	}
	if len(meta) == 0 {
		meta = nil
	}
	return event.Event{
		Severity: event.SeverityError,
		Kind:     "build_failure",
		Title:    p.msg,
		Location: loc,
		Body:     []string{p.file + ":" + strconv.Itoa(p.line) + ":" + strconv.Itoa(p.col) + ": " + p.msg},
		Metadata: meta,
	}
}
