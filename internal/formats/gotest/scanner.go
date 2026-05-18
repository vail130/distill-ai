package gotest

import (
	"bufio"
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
)

// State machine constants for the scanner. The scanner advances
// through states as it consumes lines from gotest's default reporter.
// `-v` adds a few framing lines that are dropped uniformly; the
// state machine doesn't model them explicitly.
type state int

const (
	stateRunning     state = iota // initial: before any failure block has started
	stateFailureBody              // inside a `--- FAIL:` block, accumulating Body
	statePanicBody                // inside a `panic:` block, accumulating goroutine dump
	stateRaceBody                 // inside a `==================` race report block
	stateSummary                  // after `FAIL\t<pkg>` / `PASS` / `exit status N`
)

// Pre-compiled regexes for the M10.2 scanner. Compiled once at
// package init; the scanner consults them per line.
var (
	// failHeaderLinePattern parses `--- FAIL: TestName (0.02s)` and
	// captures the test ID and duration. Indentation is allowed so
	// subtest failure headers from the default reporter still match
	// (gotest indents subtest headers two spaces beyond the parent).
	failHeaderLinePattern = regexp.MustCompile(`^(?:\s*)--- FAIL: (\S+)(?: \(([0-9.]+s)\))?\s*$`)

	// passHeaderLinePattern matches `--- PASS:` so the scanner can
	// tell a pass from a fail without re-parsing the rest of the
	// line.
	passHeaderLinePattern = regexp.MustCompile(`^(?:\s*)--- PASS: `)

	// skipHeaderLinePattern matches `--- SKIP:`.
	skipHeaderLinePattern = regexp.MustCompile(`^(?:\s*)--- SKIP: `)

	// runHeaderLinePattern matches `=== RUN   TestName` (and
	// `=== PAUSE`, `=== CONT` cousins via the `=== ` prefix). The
	// captured group is the test name.
	runHeaderLinePattern = regexp.MustCompile(`^=== RUN {3}(\S+)`)

	// frameHeaderPattern matches `=== PAUSE` / `=== CONT` so the
	// scanner can drop them without confusing them with a new test
	// start.
	frameHeaderPattern = regexp.MustCompile(`^=== (PAUSE|CONT) `)

	// packageSummaryPattern matches `FAIL\t<pkg>\t<duration>` and
	// captures the package path. Used to end a failure block and
	// to populate `metadata.package` on any in-flight Event.
	packageSummaryPattern = regexp.MustCompile(`^FAIL\t(\S+)(?:\t.+)?$`)

	// passSummaryPattern matches `ok  \t<pkg>\t...` or the bare
	// `PASS` line. Ends any in-flight failure block.
	passSummaryPattern = regexp.MustCompile(`^(?:ok {2}\t|PASS$)`)

	// exitStatusPattern matches the trailing `exit status N` line.
	exitStatusPattern = regexp.MustCompile(`^exit status \d+$`)

	// assertionLinePattern extracts a `<file>:<line>: <message>`
	// shape from a failure body line. Used to derive the Event
	// Title and Location. The path must end in `.go` or contain `/`
	// so unrelated `host:port:` shapes don't match. The leading
	// indent is stripped before matching because gotest indents
	// per-test output by tabs / spaces.
	assertionLinePattern = regexp.MustCompile(`^(\S+\.go|\S+/\S+):(\d+):\s+(.*)$`)

	// raceDividerPattern matches the `==================` lines
	// that frame a race-detector report. The race detector emits
	// two such lines for each report: one above the report, one
	// below.
	raceDividerPattern = regexp.MustCompile(`^={5,}$`)
)

// maxRaceLines caps how many lines a race-detector block may
// accumulate before truncation. Race reports include two
// goroutines' stacks plus a `Previous write` / `Goroutine N (running)`
// header per stack, so they are larger than typical panics — the cap
// is correspondingly larger than maxPanicLines.
const maxRaceLines = 300

// raceConditionTitle is the canonical first non-divider line gotest's
// race detector emits. Used as the Event Title when the report block
// is recognised.
const raceConditionTitle = "WARNING: DATA RACE"

// raceTruncatedSentinel marks Body when the maxRaceLines cap fires.
const raceTruncatedSentinel = "... [race report truncated]"

// parseStream runs the M10.2 scanner over r and forwards Events to
// out. It is the body of Format.Parse, extracted so tests can drive
// it without going through the channel-creation boilerplate. The
// caller closes out.
//
// Memory is bounded: the scanner holds at most one in-flight
// failure block at a time (Body grows line-by-line until the block
// terminates). gotest's default reporter never emits a single
// failure block longer than a few dozen lines in real code; a hard
// cap is unnecessary at this layer because callers expect bounded
// gotest output.
//
// Concurrency: the scanner runs on a single goroutine started by
// Parse. ctx is checked before each line read and before each send
// so cancellation propagates promptly.
func parseStream(ctx context.Context, r io.Reader) <-chan event.Event {
	out := make(chan event.Event, 1)
	go func() {
		defer close(out)
		_ = scanLoop(ctx, r, out)
	}()
	return out
}

// scanLoop is the state machine. It exists separate from
// parseStream so tests can call it without the goroutine /
// channel wrapper if needed; today only parseStream calls it.
//
// Per-package buffering: gotest emits the package name only on the
// trailing `FAIL\t<pkg>` summary line, after every per-test
// `--- FAIL:` block in the package. To stamp `metadata.package` on
// each emitted Event, the scanner buffers per-package pending
// failures and flushes them when the package line is consumed. The
// buffer is bounded — a Go package rarely has more than a handful
// of failing tests per run — and across packages the scanner still
// streams: the first package's events emerge as soon as that
// package's summary line arrives, while the next package's tests
// are still being scanned.
//
// On EOF without a package line (rare; truncated input or a
// non-gotest stream that happened to match the markers), the
// scanner flushes pending events with no package metadata. The
// data still beats dropping the events.
func scanLoop(ctx context.Context, r io.Reader, out chan<- event.Event) error {
	sc := bufio.NewScanner(r)
	// Allow long lines; gotest sometimes prints multi-KiB diffs in
	// failure bodies (notably from testify's deep-equal failure
	// rendering). Cap at 1 MiB to bound adversarial input.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	// Mode detection: read up to the first non-blank line. If it
	// begins with `{"Time":`, we're in `-json` mode; dispatch to the
	// JSON scanner. Otherwise the bytes we read are still
	// available — we pass them through as the first line of the
	// text-mode scanner via a helper closure.
	var firstLine []byte
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		firstLine = append([]byte(nil), raw...)
		break
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(firstLine) == 0 {
		return nil // empty input
	}
	if isJSONLine(firstLine) {
		return scanJSONLoop(ctx, sc, firstLine, out)
	}
	return scanTextLoop(ctx, sc, firstLine, out)
}

// isJSONLine reports whether b looks like a `go test -json` event
// line. The check is cheap and deliberately strict: gotest emits
// `Time` as the first key in every line. Other tools that happen to
// emit JSON-per-line use different shapes; this keeps the JSON
// dispatcher from mis-claiming them.
func isJSONLine(b []byte) bool {
	return len(b) > 9 && string(b[:9]) == `{"Time":"`
}

// scanTextLoop is the original text-mode state machine, factored
// out so scanLoop can dispatch between text and JSON modes. It
// receives the already-consumed first line and threads it into the
// loop as if it had just arrived from the scanner.
func scanTextLoop(ctx context.Context, sc *bufio.Scanner, firstLine []byte, out chan<- event.Event) error {
	st := stateRunning
	var (
		cur     *pendingFailure
		pending []*pendingFailure
		curPan  *pendingPanic
		curRace *pendingRace
		// preBody holds the indented per-test output that gotest
		// emits between `=== RUN` and `--- FAIL:`. When the FAIL
		// header arrives, those lines are prepended to the new
		// block's body so assertion lines (and their `file:line:
		// msg` shape) reach the Title / Location derivation. The
		// buffer is cleared on `=== RUN`, `--- PASS:`, `--- SKIP:`,
		// or `--- FAIL:` to avoid leaking one test's logf into a
		// later test's failure.
		preBody []string
		// curTest tracks the most recently-started test name so a
		// mid-test panic can attribute Metadata["test_id"]
		// correctly.
		curTest string
		// panickedTests holds test_ids that have already emitted
		// a panic Event. When the trailing `--- FAIL: TestName`
		// for one of those tests arrives, the scanner drops the
		// header rather than emitting a duplicate test_failure —
		// the panic carries the diagnostic and the test_failure
		// would just be a noisier rephrasing.
		panickedTests = map[string]bool{}
	)
	flushPanic := func() error {
		if curPan == nil {
			return nil
		}
		ev := finalisePanic(curPan)
		curPan = nil
		st = stateRunning
		return sendEvent(ctx, out, ev)
	}
	flushRace := func() error {
		if curRace == nil {
			return nil
		}
		ev := finaliseRace(curRace)
		curRace = nil
		st = stateRunning
		return sendEvent(ctx, out, ev)
	}
	flush := func(pkg string) error {
		if err := flushPanic(); err != nil {
			return err
		}
		if err := flushRace(); err != nil {
			return err
		}
		if cur != nil {
			pending = append(pending, cur)
			cur = nil
		}
		for _, p := range pending {
			if p.pkg == "" {
				p.pkg = pkg
			}
			if err := emit(ctx, out, p); err != nil {
				return err
			}
		}
		pending = nil
		return nil
	}
	// Replay the already-consumed firstLine before draining the
	// rest of the scanner, so dispatch and the state machine see
	// the same line sequence.
	firstUsed := false
	nextLine := func() (string, bool) {
		if !firstUsed {
			firstUsed = true
			return string(firstLine), true
		}
		if sc.Scan() {
			return sc.Text(), true
		}
		return "", false
	}
	for {
		line, ok := nextLine()
		if !ok {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Phase A: inside a race block, the line either extends
		// the block or, if it's the closing divider, terminates
		// it. The race state takes precedence over panic / fail
		// state because the race detector wraps the entire
		// contained dump in its own dividers.
		if st == stateRaceBody {
			// The closing divider terminates the race report.
			if raceDividerPattern.MatchString(line) {
				if len(curRace.body) < maxRaceLines {
					curRace.body = append(curRace.body, line)
				}
				if err := flushRace(); err != nil {
					return err
				}
				continue
			}
			switch {
			case len(curRace.body) < maxRaceLines-1:
				curRace.body = append(curRace.body, line)
			case !curRace.truncated:
				curRace.body = append(curRace.body, raceTruncatedSentinel)
				curRace.truncated = true
			}
			continue
		}
		// Phase B: inside a panic block, the line either extends
		// or terminates the block.
		if st == statePanicBody {
			if panicContinuationPattern.MatchString(line) && !raceDividerPattern.MatchString(line) {
				switch {
				case len(curPan.body) < maxPanicLines-1:
					curPan.body = append(curPan.body, line)
				case !curPan.truncated:
					// Replace the last accepted line with the
					// sentinel rather than growing beyond the
					// cap. The cap counts every Body entry
					// including the sentinel.
					curPan.body = append(curPan.body, panicTruncatedSentinel)
					curPan.truncated = true
				}
				continue
			}
			// Block terminator; flush, fall through to handle the
			// line as a normal event candidate.
			if err := flushPanic(); err != nil {
				return err
			}
		}
		// Race opener: a `==================` divider starts a
		// new race-detector block.
		if raceDividerPattern.MatchString(line) {
			// If we're inside any other state, flush first.
			if cur != nil {
				pending = append(pending, cur)
				cur = nil
			}
			curRace = &pendingRace{body: []string{line}, testID: curTest}
			// Mark the test as panicked-equivalent so the
			// trailing `--- FAIL: TestName` doesn't double up
			// the diagnostic with a redundant test_failure.
			if curTest != "" {
				panickedTests[curTest] = true
			}
			st = stateRaceBody
			continue
		}
		// Build-failure summary: `FAIL\t<pkg> [build failed]`
		// closes the build-failure stream and stamps any
		// already-emitted build_failure Events. Since they're
		// emitted streaming, stamping is best-effort: we don't
		// retroactively update Events sent to the channel. The
		// pattern is still recognised so the package metadata
		// path on the summary line doesn't false-match the
		// `packageSummaryPattern`.
		if buildFailureSummaryPattern.MatchString(line) {
			preBody = nil
			st = stateSummary
			continue
		}
		if m := packageSummaryPattern.FindStringSubmatch(line); m != nil {
			preBody = nil
			if err := flush(m[1]); err != nil {
				return err
			}
			st = stateSummary
			continue
		}
		if passSummaryPattern.MatchString(line) || exitStatusPattern.MatchString(line) {
			preBody = nil
			if cur != nil {
				pending = append(pending, cur)
				cur = nil
			}
			st = stateSummary
			continue
		}
		// Panic header: starts a panic block. If we're inside a
		// failure block, the panic is attributed to that test;
		// the test_failure Event will not also emit (the panic
		// replaces it).
		if panicHeaderPattern.MatchString(line) {
			panicTest := curTest
			if cur != nil {
				panicTest = cur.testID
				cur = nil // suppress the test_failure; panic wins
			}
			if panicTest != "" {
				panickedTests[panicTest] = true
			}
			preBody = nil
			curPan = &pendingPanic{header: line, body: []string{line}, testID: panicTest}
			st = statePanicBody
			continue
		}
		// Build-error line: emit a build_failure Event eagerly.
		// These don't accumulate; one Event per matched location.
		if m := buildErrorLinePattern.FindStringSubmatch(line); m != nil {
			ln, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			pb := &pendingBuild{
				file: m[1],
				line: ln,
				col:  col,
				msg:  strings.TrimSpace(m[4]),
			}
			if err := sendEvent(ctx, out, finaliseBuild(pb)); err != nil {
				return err
			}
			continue
		}
		if m := runHeaderLinePattern.FindStringSubmatch(line); m != nil {
			curTest = m[1]
			preBody = nil
			continue
		}
		if frameHeaderPattern.MatchString(line) {
			continue
		}
		if m := failHeaderLinePattern.FindStringSubmatch(line); m != nil {
			if panickedTests[m[1]] {
				// A panic already accounted for this test;
				// drop the duplicate FAIL header so we don't
				// emit a redundant test_failure.
				preBody = nil
				st = stateRunning
				continue
			}
			if cur != nil {
				pending = append(pending, cur)
			}
			body := make([]string, 0, len(preBody)+1)
			body = append(body, preBody...)
			body = append(body, line)
			cur = &pendingFailure{
				testID:   m[1],
				duration: m[2],
				headerLn: line,
				body:     body,
			}
			curTest = m[1]
			preBody = nil
			st = stateFailureBody
			continue
		}
		if passHeaderLinePattern.MatchString(line) || skipHeaderLinePattern.MatchString(line) {
			preBody = nil
			if cur != nil {
				pending = append(pending, cur)
				cur = nil
			}
			st = stateRunning
			continue
		}
		if cur != nil && st == stateFailureBody {
			cur.body = append(cur.body, line)
			continue
		}
		if st == stateRunning && isIndented(line) {
			preBody = append(preBody, line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush("")
}

// sendEvent forwards ev to out, honouring ctx.
func sendEvent(ctx context.Context, out chan<- event.Event, ev event.Event) error {
	select {
	case out <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isIndented reports whether line starts with a space or tab.
// gotest indents per-test output (assertions, `t.Logf` output)
// uniformly; the scanner uses indentation as a cheap proxy for
// "this line belongs to the currently-running test."
func isIndented(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	return c == ' ' || c == '\t'
}

// pendingFailure is the in-flight Event for a `--- FAIL:` block.
// The scanner accumulates body lines until the block terminator;
// at emit time the fields are projected into event.Event.
type pendingFailure struct {
	testID   string
	duration string
	pkg      string
	headerLn string
	body     []string
}

// emit converts a pendingFailure into an event.Event and sends it
// to out, honouring ctx so cancellation propagates cleanly.
func emit(ctx context.Context, out chan<- event.Event, p *pendingFailure) error {
	ev := buildFailureEvent(p)
	select {
	case out <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// buildFailureEvent projects the accumulated state into a final
// event.Event. Title is derived from the first body line that
// looks like a `file.go:line: msg` assertion, falling back to the
// trimmed `--- FAIL:` header. Location comes from the same
// assertion line.
func buildFailureEvent(p *pendingFailure) event.Event {
	title := strings.TrimSpace(p.headerLn)
	var loc *event.Location
	for _, line := range p.body {
		stripped := strings.TrimLeft(line, " \t")
		m := assertionLinePattern.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}
		ln, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		loc = &event.Location{File: m[1], Line: ln}
		if msg := strings.TrimSpace(m[3]); msg != "" {
			title = msg
		} else {
			title = stripped
		}
		break
	}
	meta := map[string]string{}
	if p.testID != "" {
		meta["test_id"] = p.testID
	}
	if p.pkg != "" {
		meta["package"] = p.pkg
	}
	if p.duration != "" {
		meta["duration"] = p.duration
	}
	if len(meta) == 0 {
		meta = nil
	}
	return event.Event{
		Severity: event.SeverityError,
		Kind:     "test_failure",
		Title:    title,
		Location: loc,
		Body:     append([]string(nil), p.body...),
		Metadata: meta,
	}
}
