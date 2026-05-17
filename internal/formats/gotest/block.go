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
	//     "created by" lines, function-arg lines.
	//   - `^$` — blank lines inside the dump.
	//   - `^goroutine \d+ ` — goroutine headers.
	//   - `^panic: ` — chained panics from goroutines.
	//   - `^\[` — signal subheaders like `[signal SIGSEGV: ...]`,
	//     `[recovered]`.
	//   - `^[\w./*()]+\([\w *,.\-]*\)$` — Go function-call lines
	//     (`pkg.Func(args)`, `(*T).method(args)`).
	panicContinuationPattern = regexp.MustCompile(
		`^(?:\s|$|goroutine \d+ |panic: |\[|[\w./*()]+\([\w *,.\-]*\)$)`,
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
// dump. M10.3 leaves Frames nil; M10.4 wires the goroutine-frame
// extractor.
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
	return event.Event{
		Severity: event.SeverityError,
		Kind:     "panic",
		Title:    strings.TrimSpace(p.header),
		Body:     append([]string(nil), p.body...),
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
