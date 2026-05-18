package jest

import (
	"bufio"
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
)

// State machine constants. The scanner advances through these
// states as it consumes lines from jest's default or `--verbose`
// reporter. The two reporters produce the same anchor markers
// (`●`, FAIL/PASS headers); `--verbose` only inserts `✓`/`✗`
// indicator lines that the scanner drops uniformly.
type state int

const (
	stateRunning       state = iota // initial: lines before first ● or FAIL header
	stateFailureHeader              // consumed a ● header, next line opens the body
	stateFailureBody                // accumulating block body until terminator
	stateSummary                    // post-`Test Suites:` / `Tests:` lines; discarded
)

// Pre-compiled regexes for the M12.2 scanner. Compiled once at
// package init; the scanner consults them per line.
var (
	// bulletHeaderLinePattern matches the `●` failure block
	// header. Optional leading whitespace because jest indents
	// the bullet two spaces in some reporter configs. The
	// captured group is the trimmed test-path text — the
	// `Suite › Test name` chain jest renders after the bullet.
	bulletHeaderLinePattern = regexp.MustCompile(`^\s*●\s+(.+?)\s*$`)

	// failPerFileHeaderPattern matches the per-file header jest
	// emits before each test file's results: `FAIL src/auth.test.js`.
	// Captured groups: status (`FAIL`/`PASS`) and path. The path
	// is whatever-not-whitespace; we trust the M12.1 detector to
	// have classified the run as jest before reaching the
	// scanner, so we accept any path-shaped token without
	// re-running the path-token guard.
	failPerFileHeaderPattern = regexp.MustCompile(`^(FAIL|PASS)\s+(\S+)`)

	// testSuitesSummaryPattern matches `Test Suites: ...` which
	// jest emits at the end of every run. Terminates any
	// in-flight failure block and moves the scanner into
	// stateSummary.
	testSuitesSummaryPattern = regexp.MustCompile(`^Test Suites:\s+`)

	// testsSummaryLinePattern matches the `Tests:` summary line.
	// Same terminator role as testSuitesSummaryPattern; both
	// shapes appear in jest output.
	testsSummaryLinePattern = regexp.MustCompile(`^Tests:\s+`)

	// stackFrameWithFnPattern matches an indented jest stack
	// frame with a function name:
	//
	//   at functionName (path/to/file.js:line:col)
	//
	// Captures function (1), path (2), line (3), col (4). Used
	// by M12.4 to populate Event.Frames and by locationFromBody
	// to derive Location from the first frame.
	stackFrameWithFnPattern = regexp.MustCompile(`^\s+at\s+(\S+(?:\s\S+)*)\s+\((\S+?):(\d+):(\d+)\)\s*$`)

	// stackFrameNoFnPattern matches the no-function-name frame
	// shape common in async / bundled output:
	//
	//   at path/to/file.js:line:col
	//
	// Captures path (1), line (2), col (3).
	stackFrameNoFnPattern = regexp.MustCompile(`^\s+at\s+(\S+?):(\d+):(\d+)\s*$`)

	// expectAssertionPattern matches an `expect(...).toBe(...)`-
	// style assertion call appearing on its own line. Used to
	// derive Event Title when present.
	expectAssertionPattern = regexp.MustCompile(`^\s*(expect\([^)]*\)\.\S+)`)

	// expectedReceivedPattern matches the `Expected: ...` /
	// `Received: ...` shape jest's expect rendering uses. The
	// scanner emits the Expected line (when found) as the
	// Title when no expect() call is present.
	expectedReceivedPattern = regexp.MustCompile(`^\s*Expected:\s+(.+?)\s*$`)

	// errorClassPattern matches an `Error: <msg>` or
	// `<Class>Error: <msg>` shape; the fallback Title
	// derivation when no expect() / Expected: line was found.
	// `Error` alone counts (no required prefix) so bare
	// `Error: timeout` lines are recognised as Title-worthy.
	errorClassPattern = regexp.MustCompile(`^\s*((?:\w*Error|AssertionError):\s+.+?)\s*$`)

	// ansiEscapePattern strips ANSI colour / format escape
	// sequences from Title text. Body retains them so the user
	// sees what jest actually emitted; matches the M9.2 generic
	// scanner's convention.
	ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

	// verboseIndicatorPattern matches the `✓` and `✗` per-test
	// indicator lines `--verbose` reporters insert before the
	// failure summary. Dropped uniformly.
	verboseIndicatorPattern = regexp.MustCompile(`^\s*[✓✗]\s+`)

	// snapshotFilePattern matches the `expect(...).toMatchSnapshot(...)`
	// assertion call. The opening `expect(...)` argument list is
	// captured as one group so the precedence over the more
	// generic expectAssertionPattern is unambiguous; only the
	// `.toMatchSnapshot` suffix is used in the dispatch decision.
	snapshotFilePattern = regexp.MustCompile(`expect\([^)]*\)\.toMatchSnapshot\(`)

	// snapshotInlinePattern matches the inline variant. Mirrors
	// snapshotFilePattern but the suffix differs.
	snapshotInlinePattern = regexp.MustCompile(`expect\([^)]*\)\.toMatchInlineSnapshot\(`)

	// snapshotNamePattern matches the `Snapshot name: \`<name>\`` line
	// jest emits beneath a file-backed snapshot assertion. The
	// captured group is the trimmed name text, used as the
	// per-Event Title when the snapshot is file-backed.
	snapshotNamePattern = regexp.MustCompile("^\\s*Snapshot name:\\s+`(.+?)`\\s*$")
)

// maxSnapshotLines caps how many lines a snapshot-block Event's
// Body retains before truncation. Snapshot diffs can run to
// hundreds of lines for large fixtures; the cap mirrors M9.3 /
// M10.3's similar caps on traceback and panic blocks.
const maxSnapshotLines = 200

// snapshotTruncatedSentinel replaces the last accepted Body entry
// when the maxSnapshotLines cap fires. Surfaces in the rendered
// Event so the user knows the diff was cut.
const snapshotTruncatedSentinel = "... [snapshot truncated]"

// unicodeChevron is jest's per-suite path separator (U+203A SINGLE
// RIGHT-POINTING ANGLE QUOTATION MARK). M12.2 normalises it to a
// plain ASCII `>` in Metadata["test_id"] so the value is grep-able
// across tools and editors that may not render the Unicode glyph.
const (
	unicodeChevron = "›"
	asciiChevron   = ">"
)

// parseStream runs the M12.2 scanner over r and forwards Events to
// the returned channel. The channel is closed when r reaches EOF,
// when ctx is cancelled, or when an unrecoverable I/O error
// occurs.
//
// Memory is bounded: the scanner holds at most one in-flight block
// at a time. The bufio.Scanner buffer caps at 1 MiB so adversarial
// inline-snapshot diffs cannot blow the heap.
//
// Concurrency: the scanner runs on a single goroutine started by
// Parse. ctx is checked before each line read and before each
// send so cancellation propagates promptly.
func parseStream(ctx context.Context, r io.Reader) <-chan event.Event {
	out := make(chan event.Event, 1)
	go func() {
		defer close(out)
		_ = scanLoop(ctx, r, out)
	}()
	return out
}

// scanLoop is the state machine. Extracted from parseStream so
// tests can call it without the goroutine wrapper if needed.
func scanLoop(ctx context.Context, r io.Reader, out chan<- event.Event) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	st := stateRunning
	var (
		cur          *pendingFailure
		curSuiteFile string
	)
	flush := func() error {
		if cur == nil {
			return nil
		}
		ev := buildFailureEvent(cur)
		cur = nil
		return sendEvent(ctx, out, ev)
	}
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := sc.Text()
		// stateSummary swallows everything until EOF.
		if st == stateSummary {
			continue
		}
		// Per-file FAIL/PASS header. Both close any in-flight
		// failure block (a per-file header marks the end of the
		// previous file's failures) and update curSuiteFile so
		// subsequent ● Events carry the right path metadata.
		if m := failPerFileHeaderPattern.FindStringSubmatch(line); m != nil {
			if err := flush(); err != nil {
				return err
			}
			curSuiteFile = m[2]
			st = stateRunning
			continue
		}
		// `Test Suites:` / `Tests:` summary lines terminate the
		// entire run.
		if testSuitesSummaryPattern.MatchString(line) ||
			testsSummaryLinePattern.MatchString(line) {
			if err := flush(); err != nil {
				return err
			}
			st = stateSummary
			continue
		}
		// Bullet header opens a new failure block. If one was
		// already in flight, flush it first. ANSI escapes are
		// stripped before pattern matching because jest's
		// default reporter wraps the bullet itself in colour
		// escapes (`\x1b[31m●\x1b[0m`), and the bare anchor must
		// detect on both coloured and CI (no-ANSI) renderings.
		// The raw line — escapes intact — is what we store in
		// Body so the user sees what jest emitted.
		if m := bulletHeaderLinePattern.FindStringSubmatch(stripANSI(line)); m != nil {
			if err := flush(); err != nil {
				return err
			}
			cur = &pendingFailure{
				headerLn:  line,
				testPath:  strings.TrimSpace(m[1]),
				suiteFile: curSuiteFile,
				body:      []string{line},
			}
			st = stateFailureHeader
			continue
		}
		// Drop `--verbose` per-test indicator lines so they don't
		// leak into the next block's Body.
		if verboseIndicatorPattern.MatchString(line) {
			continue
		}
		// Inside a failure block, accumulate body lines verbatim.
		if st == stateFailureHeader || st == stateFailureBody {
			cur.body = append(cur.body, line)
			st = stateFailureBody
			continue
		}
		// stateRunning, non-matching line — drop. This covers
		// console.log noise between tests, coverage tables, and
		// any other framing the parser doesn't anchor on.
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

// pendingFailure is the in-flight Event for a `●` failure block.
// The scanner accumulates body lines until the block terminator;
// at emit time the fields are projected into event.Event.
type pendingFailure struct {
	headerLn  string
	testPath  string
	suiteFile string
	body      []string
}

// buildFailureEvent projects the accumulated state into a final
// event.Event. The default Kind is `test_failure`; the function
// promotes the Kind to `snapshot_mismatch` when the body contains
// a `toMatchSnapshot` or `toMatchInlineSnapshot` assertion, and
// applies the maxSnapshotLines cap to Body in that case.
func buildFailureEvent(p *pendingFailure) event.Event {
	kind, snapKind, snapName := classifySnapshot(p.body)
	body := append([]string(nil), p.body...)
	truncated := false
	if kind == "snapshot_mismatch" && len(body) > maxSnapshotLines {
		body = append(body[:maxSnapshotLines-1], snapshotTruncatedSentinel)
		truncated = true
	}
	// suite_error promotion: a `●` header whose path is the
	// suite file itself (with no test-name continuation) or the
	// special heading "Test suite failed to run" identifies a
	// failure outside any individual test. Suite errors don't
	// have a per-test identity, so test_id is not emitted.
	suiteError := isSuiteErrorHeader(p.testPath, p.suiteFile)
	if suiteError {
		kind = "suite_error"
	}
	title := snapshotTitle(kind, snapName, p.body, p.testPath)
	loc := locationFromBody(p.body)
	frames := framesFromBody(p.body)
	meta := map[string]string{}
	if !suiteError && p.testPath != "" {
		// Normalise the Unicode chevron jest renders between
		// suite and test names to ASCII `>`. The result is
		// grep-able from any terminal / editor. Suppressed for
		// suite errors because they have no test_id.
		meta["test_id"] = strings.ReplaceAll(p.testPath, unicodeChevron, asciiChevron)
	}
	if p.suiteFile != "" {
		meta["suite_file"] = p.suiteFile
	}
	if snapKind != "" {
		meta["snapshot_kind"] = snapKind
	}
	if truncated {
		meta["snapshot_truncated"] = "true"
	}
	if len(meta) == 0 {
		meta = nil
	}
	return event.Event{
		Severity: event.SeverityError,
		Kind:     kind,
		Title:    title,
		Location: loc,
		Body:     body,
		Frames:   frames,
		Metadata: meta,
	}
}

// isSuiteErrorHeader reports whether a `●` block header identifies
// a suite-level failure rather than a per-test failure. Two
// signals are checked:
//
//   - The header text exactly matches `Test suite failed to run`
//     (jest's canonical phrasing).
//   - The header text exactly matches the per-file suite path
//     (no `›`-separated test-name continuation). When suiteFile
//     is set and the header text equals it, the failure ran
//     before any individual test could be selected.
func isSuiteErrorHeader(testPath, suiteFile string) bool {
	if testPath == "Test suite failed to run" {
		return true
	}
	if suiteFile != "" && testPath == suiteFile {
		return true
	}
	return false
}

// classifySnapshot inspects body for a `toMatchSnapshot` or
// `toMatchInlineSnapshot` assertion call. Returns the Event Kind
// to use (`test_failure` or `snapshot_mismatch`), the snapshot
// variant (`"file"`, `"inline"`, or `""` for non-snapshot), and
// the file-backed snapshot name when present. ANSI escapes are
// stripped before pattern matching.
func classifySnapshot(body []string) (kind, snapKind, snapName string) {
	for _, line := range body {
		stripped := stripANSI(line)
		switch {
		case snapshotFilePattern.MatchString(stripped):
			return "snapshot_mismatch", "file", findSnapshotName(body)
		case snapshotInlinePattern.MatchString(stripped):
			return "snapshot_mismatch", "inline", ""
		}
	}
	return "test_failure", "", ""
}

// findSnapshotName returns the trimmed name from the first
// `Snapshot name: \`<name>\“ line in body, or "" if absent.
// ANSI escapes are stripped before matching.
func findSnapshotName(body []string) string {
	for _, line := range body {
		if m := snapshotNamePattern.FindStringSubmatch(stripANSI(line)); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// snapshotTitle derives the Title for a classified Event. For
// non-snapshot failures it falls back to titleFromBody; for
// file-backed snapshots it uses the snapshot name when one was
// found, falling back to the generic form; for inline snapshots
// the generic form is the only option because jest does not
// print a name.
func snapshotTitle(kind, snapName string, body []string, fallback string) string {
	if kind != "snapshot_mismatch" {
		return titleFromBody(body, fallback)
	}
	if snapName != "" {
		return "Snapshot mismatch: " + snapName
	}
	return "Snapshot mismatch"
}

// titleFromBody derives an Event Title by walking the captured
// body lines in order and returning the first match against the
// expect / Expected: / error-class precedence. Falls back to the
// trimmed test-path text from the `●` header. ANSI escape
// sequences are stripped before pattern matching so coloured
// default-reporter output and plain CI-reporter output produce
// the same Title.
func titleFromBody(body []string, fallback string) string {
	for _, line := range body {
		stripped := stripANSI(line)
		if m := expectAssertionPattern.FindStringSubmatch(stripped); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	for _, line := range body {
		stripped := stripANSI(line)
		if m := expectedReceivedPattern.FindStringSubmatch(stripped); m != nil {
			return "Expected: " + strings.TrimSpace(m[1])
		}
	}
	for _, line := range body {
		stripped := stripANSI(line)
		if m := errorClassPattern.FindStringSubmatch(stripped); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return stripANSI(strings.TrimSpace(fallback))
}

// locationFromBody returns an event.Location populated from the
// first stack frame found in body, or nil if no frame matches.
// Both `at fn (path:line:col)` and `at path:line:col` shapes are
// recognised; the path filter is identical to the M9.2 generic
// scanner's "must contain a slash or end in a JS-family extension"
// heuristic, applied transitively because both regexes already
// match jest's frame shape.
func locationFromBody(body []string) *event.Location {
	for _, line := range body {
		if m := stackFrameWithFnPattern.FindStringSubmatch(line); m != nil {
			// Captures: 1=function, 2=path, 3=line, 4=col.
			ln, err := strconv.Atoi(m[3])
			if err != nil {
				continue
			}
			col, _ := strconv.Atoi(m[4])
			return &event.Location{File: m[2], Line: ln, Column: &col}
		}
		if m := stackFrameNoFnPattern.FindStringSubmatch(line); m != nil {
			ln, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			col, _ := strconv.Atoi(m[3])
			return &event.Location{File: m[1], Line: ln, Column: &col}
		}
	}
	return nil
}

// framesFromBody returns all stack frames extracted from body, in
// source order. Each match against stackFrameWithFnPattern produces
// a frame with Function set; stackFrameNoFnPattern produces a frame
// without. Vendor is left false — the M5 CollapseStage's
// ClassifyFrames re-populates it via the `node_modules/` pattern
// catalogue.
//
// Returns nil when no frames match so encoders see a consistent
// "no frames" signal rather than an empty slice. Matches the
// M10 / M11 convention.
func framesFromBody(body []string) []event.StackFrame {
	var out []event.StackFrame
	for _, line := range body {
		if m := stackFrameWithFnPattern.FindStringSubmatch(line); m != nil {
			ln, err := strconv.Atoi(m[3])
			if err != nil {
				continue
			}
			out = append(out, event.StackFrame{
				Function: m[1],
				File:     m[2],
				Line:     ln,
			})
			continue
		}
		if m := stackFrameNoFnPattern.FindStringSubmatch(line); m != nil {
			ln, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			out = append(out, event.StackFrame{
				File: m[1],
				Line: ln,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stripANSI removes ANSI colour / format escape sequences from s.
// Cheap; the scanner only calls it on a per-Event Title, not per
// body line.
func stripANSI(s string) string { return ansiEscapePattern.ReplaceAllString(s, "") }

// sendEvent forwards ev to out, honouring ctx so cancellation
// propagates cleanly.
func sendEvent(ctx context.Context, out chan<- event.Event, ev event.Event) error {
	select {
	case out <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
