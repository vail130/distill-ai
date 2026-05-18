package pytest

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
// through states as it consumes lines from pytest's default
// reporter. Verbose (`-v`) and non-verbose runs share the same
// state machine; the only difference is the collection lines at
// the top, which are discarded in stateSession either way.
type state int

const (
	stateSession    state = iota // initial: before the first section banner
	stateBlockEntry              // matched a section banner, awaiting `___ id ___`
	stateBlockBody               // accumulating a block's body
	stateWarnings                // inside `=== warnings summary ===`, awaiting warning entries
	stateSummary                 // post-summary banner; everything discarded
)

// blockKind tracks which section the in-flight block lives under.
// pytest emits per-section banners (`=== FAILURES ===`,
// `=== ERRORS ===`) and the contents share an identical shape; only
// the Event Kind differs at emit time.
type blockKind int

const (
	blockFailure         blockKind = iota // FAILURES section → test_failure
	blockError                            // ERRORS section, per-test → test_error
	blockCollectionError                  // ERRORS section, collection phase → collection_error
)

// Pre-compiled regexes. Compiled once at package init; the scanner
// consults them per line.
var (
	// failuresBannerLinePattern matches the `=== FAILURES ===`
	// section divider at start-of-line. Width of the `=` run is
	// variable across pytest versions, hence the `=+` quantifier.
	failuresBannerLinePattern = regexp.MustCompile(`^=+\s+FAILURES\s+=+\s*$`)

	// errorsBannerLinePattern matches the `=== ERRORS ===` section
	// divider. ERRORS hosts both per-test errors (fixture
	// failures, mid-test exceptions during setup) and collection-
	// phase errors (import errors, conftest syntax errors). The
	// underline text distinguishes the two; see
	// collectionUnderlinePattern.
	errorsBannerLinePattern = regexp.MustCompile(`^=+\s+ERRORS\s+=+\s*$`)

	// shortSummaryBannerPattern matches the
	// `=== short test summary info ===` banner that terminates
	// both FAILURES and ERRORS sections.
	shortSummaryBannerPattern = regexp.MustCompile(`^=+\s+short test summary info\s+=+\s*$`)

	// trailingResultPattern matches the `=== 1 failed, 2 passed
	// in 0.42s ===` final line. Also a terminator for any in-
	// flight block.
	trailingResultPattern = regexp.MustCompile(`^=+\s+\d+\s+(?:failed|passed|error|skipped|warning|deselected|xfailed|xpassed)`)

	// blockUnderlinePattern matches an underlined header line:
	// `___ id ___`. pytest emits these with at least three
	// underscores on each side. The captured group is the trimmed
	// id text — a test_id for failure/error blocks, or a "ERROR
	// collecting <file>" form for collection-phase errors.
	blockUnderlinePattern = regexp.MustCompile(`^_{3,}\s+(.+?)\s+_{3,}\s*$`)

	// collectionUnderlinePattern matches the specific underlined
	// header pytest emits inside ERRORS for collection-phase
	// failures: `ERROR collecting <path>`. Captures the path.
	collectionUnderlinePattern = regexp.MustCompile(`^ERROR collecting\s+(\S+)`)

	// assertionErrorPattern matches a pytest assertion-detail line
	// (the `E   <message>` shape pytest emits beneath a `>` line
	// for long-form tracebacks). Captured group is the trimmed
	// message text.
	assertionErrorPattern = regexp.MustCompile(`^E\s+(.+?)\s*$`)

	// locationLinePattern matches a `path:line: <message>` line
	// pytest prints at the bottom of each failure block. Path
	// must contain a `/` or `\` (or end in `.py`) so unrelated
	// `host:port` references don't false-positive. Captured
	// groups are: path, line, message (optional).
	locationLinePattern = regexp.MustCompile(`^(\S+\.py|\S+[/\\]\S+):(\d+):\s*(.*)$`)

	// pythonTracebackFramePattern matches the canonical Python
	// traceback frame shape pytest emits under `--tb=long` and
	// `--tb=native`:
	//
	//   File "<path>", line <N>, in <func>
	//
	// Captures path, line, and function. Anchored so it works
	// against trimmed and indented forms (pytest indents
	// traceback frames by four spaces under --tb=long, while
	// --tb=native emits them flush-left).
	pythonTracebackFramePattern = regexp.MustCompile(`(?:^|\s)File "([^"]+)", line (\d+), in (\S+)`)

	// shortTracebackFramePattern matches the compact frame shape
	// pytest emits under `--tb=short`:
	//
	//   <path>:<line>: in <func>
	//
	// Captures path, line, function. Distinct from the trailing
	// `<path>:<line>: <message>` summary because of the `in `
	// prefix on the third field.
	shortTracebackFramePattern = regexp.MustCompile(`^(\S+\.py|\S+[/\\]\S+):(\d+):\s+in\s+(\S+)`)

	// warningsBannerLinePattern matches `=== warnings summary ===`.
	warningsBannerLinePattern = regexp.MustCompile(`^=+\s+warnings summary\s+=+\s*$`)

	// warningEntryHeaderPattern matches the unindented header
	// pytest emits per warning entry under the warnings summary
	// banner. Two shapes:
	//
	//   tests/test_a.py:10           (file:line, no trailing :)
	//   tests/test_a.py::test_foo    (file::test_id form)
	//
	// The indented body line that follows carries the warning
	// class and message. We anchor on an unindented line whose
	// first token is a `.py`-ending path so we don't confuse
	// indented continuation lines for new entries.
	warningEntryHeaderPattern = regexp.MustCompile(`^(\S+\.py)(?::(\d+))?(?:::(\S+))?\s*$`)

	// warningClassLinePattern matches a line whose leading word is
	// a Python warning class identifier ending in `Warning:`.
	// Used to derive the warning Title.
	warningClassLinePattern = regexp.MustCompile(`(\b\w+Warning):\s+(.+?)\s*$`)
)

// parseStream runs the scanner over r and forwards Events to the
// returned channel. Closes the channel when r reaches EOF, when
// ctx is cancelled, or when an unrecoverable I/O error occurs.
//
// Memory is bounded: the scanner holds at most one in-flight block
// at a time (Body grows line by line until the block terminates).
// The bufio.Scanner buffer caps at 1 MiB so adversarial
// assertrewrite output cannot blow the heap.
//
// Filtering: parseStream honours opts.MinSeverity and
// opts.KeepWarnings. The default (zero ParseOpts) drops every
// `=== warnings summary ===` block but keeps all `=== FAILURES ===`
// and `=== ERRORS ===` blocks. Setting KeepWarnings=true or
// MinSeverity=warn|info also emits warning Events.
//
// Concurrency: the scanner runs on a single goroutine started by
// Parse. ctx is checked before each line read and before each send
// so cancellation propagates promptly.
func parseStream(ctx context.Context, r io.Reader, minSeverity event.Severity) <-chan event.Event {
	out := make(chan event.Event, 1)
	go func() {
		defer close(out)
		_ = scanLoop(ctx, r, out, minSeverity)
	}()
	return out
}

// scanLoop is the state machine. Extracted from parseStream so
// tests can call it without the goroutine / channel wrapper if
// needed; today only parseStream calls it.
//
// The seenFailures flag tracks whether a FAILURES section has been
// observed yet. ERRORS that appears *before* any FAILURES means
// collection failed and tests never ran — every block beneath that
// banner emits as `collection_error`. ERRORS *after* FAILURES
// (less common; some pytest configurations emit both sections)
// reports per-test errors and emits as `test_error`. The
// distinction is also overridden per-block by the `ERROR
// collecting <path>` underline shape, which always means a
// collection-phase error regardless of section order.
func scanLoop(ctx context.Context, r io.Reader, out chan<- event.Event, minSeverity event.Severity) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	st := stateSession
	var (
		cur            *pendingBlock
		currentSection blockKind
		seenFailures   bool
		curWarn        *pendingWarning
	)
	flush := func() error {
		if cur == nil {
			return nil
		}
		ev := buildEvent(cur)
		cur = nil
		if !severityAtLeast(ev.Severity, minSeverity) {
			return nil
		}
		return sendEvent(ctx, out, ev)
	}
	flushWarn := func() error {
		if curWarn == nil {
			return nil
		}
		ev := buildWarningEvent(curWarn)
		curWarn = nil
		if !severityAtLeast(ev.Severity, minSeverity) {
			return nil
		}
		return sendEvent(ctx, out, ev)
	}
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := sc.Text()
		// Summary banners always terminate any in-flight block,
		// regardless of state.
		if shortSummaryBannerPattern.MatchString(line) ||
			trailingResultPattern.MatchString(line) {
			if err := flush(); err != nil {
				return err
			}
			if err := flushWarn(); err != nil {
				return err
			}
			st = stateSummary
			continue
		}
		// Section banner transitions can fire from any state
		// other than stateSummary. Flush in-flight then open a
		// new section.
		if failuresBannerLinePattern.MatchString(line) {
			if err := flush(); err != nil {
				return err
			}
			if err := flushWarn(); err != nil {
				return err
			}
			currentSection = blockFailure
			seenFailures = true
			st = stateBlockEntry
			continue
		}
		if errorsBannerLinePattern.MatchString(line) {
			if err := flush(); err != nil {
				return err
			}
			if err := flushWarn(); err != nil {
				return err
			}
			if seenFailures {
				currentSection = blockError
			} else {
				currentSection = blockCollectionError
			}
			st = stateBlockEntry
			continue
		}
		if warningsBannerLinePattern.MatchString(line) {
			if err := flush(); err != nil {
				return err
			}
			st = stateWarnings
			continue
		}
		switch st {
		case stateSession:
			// Drop everything until a section banner fires.
		case stateBlockEntry:
			// Looking for the first `___ id ___` underline.
			// Anything else is dropped (blank line between
			// banner and first header is common).
			if m := blockUnderlinePattern.FindStringSubmatch(line); m != nil {
				cur = newBlock(line, m[1], currentSection)
				st = stateBlockBody
			}
		case stateBlockBody:
			// Another underlined header terminates the current
			// block and starts a new one within the same
			// section.
			if m := blockUnderlinePattern.FindStringSubmatch(line); m != nil {
				if err := flush(); err != nil {
					return err
				}
				cur = newBlock(line, m[1], currentSection)
				continue
			}
			// Any other `=== ... ===` section banner not handled
			// above terminates the current section.
			if isUnknownSectionBanner(line) {
				if err := flush(); err != nil {
					return err
				}
				st = stateSummary
				continue
			}
			cur.body = append(cur.body, line)
		case stateWarnings:
			// Each warning starts on an unindented header line
			// (`<path>.py`, `<path>.py:<line>`, or
			// `<path>.py::test_id`) and ends at the next such
			// header, the next section banner, or EOF. Indented
			// continuation lines carry the actual `WarningClass:
			// message` detail and are folded into the current
			// entry's body.
			if !isIndented(line) {
				if m := warningEntryHeaderPattern.FindStringSubmatch(line); m != nil {
					if err := flushWarn(); err != nil {
						return err
					}
					ln, _ := strconv.Atoi(m[2])
					curWarn = &pendingWarning{
						file: m[1],
						line: ln,
						body: []string{line},
					}
					continue
				}
			}
			if isUnknownSectionBanner(line) {
				if err := flushWarn(); err != nil {
					return err
				}
				st = stateSummary
				continue
			}
			if curWarn != nil {
				curWarn.body = append(curWarn.body, line)
			}
		case stateSummary:
			// Everything past the summary banner is discarded.
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	return flushWarn()
}

// newBlock constructs a pendingBlock from the underline match. If
// the id text starts with `ERROR collecting`, the block is
// reclassified as collection_error regardless of which section it
// appears under — pytest emits per-file collection failures inside
// ERRORS even when FAILURES has already fired.
func newBlock(headerLn, id string, sectionKind blockKind) *pendingBlock {
	id = strings.TrimSpace(id)
	kind := sectionKind
	collectingFile := ""
	if m := collectionUnderlinePattern.FindStringSubmatch(id); m != nil {
		kind = blockCollectionError
		collectingFile = m[1]
	}
	return &pendingBlock{
		id:             id,
		kind:           kind,
		collectingFile: collectingFile,
		headerLn:       headerLn,
		body:           []string{headerLn},
	}
}

// isIndented reports whether line starts with whitespace. pytest
// uses indentation to mark continuation content within a warning
// entry (the indented `WarningClass: message` line under the
// unindented `<path>.py:<line>` header).
func isIndented(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	return c == ' ' || c == '\t'
}

// isUnknownSectionBanner reports whether line looks like a
// `=== <name> ===` divider for a section the scanner does not
// recognise as FAILURES / ERRORS / short-summary. Used to
// terminate an in-flight block cleanly when pytest emits a section
// the scanner does not parse (e.g. `=== warnings summary ===`).
func isUnknownSectionBanner(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "==") || !strings.HasSuffix(trimmed, "==") {
		return false
	}
	// A bare `===` with no inner text is not a banner.
	inner := strings.Trim(trimmed, "= ")
	return inner != ""
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

// pendingBlock is the in-flight Event for one FAILURES / ERRORS
// block. The scanner accumulates body lines until the block
// terminator; at emit time the fields project into event.Event.
type pendingBlock struct {
	id             string
	kind           blockKind
	collectingFile string // only set when kind == blockCollectionError
	headerLn       string
	body           []string
}

// buildEvent projects the accumulated state into a final
// event.Event. Title and Location derivation mirrors the M11.2
// shape; the Kind dispatch lives here so the state machine itself
// stays kind-agnostic. M11.4 adds Frame extraction from the body.
func buildEvent(p *pendingBlock) event.Event {
	title := p.id
	if title == "" {
		title = strings.TrimSpace(p.headerLn)
	}
	for _, line := range p.body {
		if m := assertionErrorPattern.FindStringSubmatch(line); m != nil {
			title = strings.TrimSpace(m[1])
			break
		}
	}
	var loc *event.Location
	// Scan bottom-up: pytest's `path:line: <message>` summary
	// appears at or near the end of the block. Taking the last
	// match avoids picking up earlier traceback frames that point
	// at fixture or helper code.
	for i := len(p.body) - 1; i >= 0; i-- {
		m := locationLinePattern.FindStringSubmatch(p.body[i])
		if m == nil {
			continue
		}
		ln, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		loc = &event.Location{File: m[1], Line: ln}
		break
	}
	frames := extractFrames(p.body)
	meta := map[string]string{}
	kindStr := "test_failure"
	switch p.kind {
	case blockFailure:
		kindStr = "test_failure"
		if p.id != "" {
			meta["test_id"] = p.id
		}
	case blockError:
		kindStr = "test_error"
		if p.id != "" {
			meta["test_id"] = p.id
		}
	case blockCollectionError:
		kindStr = "collection_error"
		// Collection errors have no per-test scope; test_id is
		// omitted. The id text (often `ERROR collecting <path>`)
		// becomes the Title fallback only; the path lands on
		// Location if a path:line: line is present further down.
		if p.collectingFile != "" && loc == nil {
			loc = &event.Location{File: p.collectingFile}
		}
	}
	if len(meta) == 0 {
		meta = nil
	}
	return event.Event{
		Severity: event.SeverityError,
		Kind:     kindStr,
		Title:    title,
		Location: loc,
		Body:     append([]string(nil), p.body...),
		Frames:   frames,
		Metadata: meta,
	}
}

// extractFrames walks body lines and pulls out structured stack
// frames in source order. Handles two shapes:
//
//   - Long-form / native: `File "<path>", line N, in <func>` — the
//     canonical Python traceback frame. Pytest's `--tb=long` and
//     `--tb=native` both emit this form (long indents with four
//     spaces, native emits flush-left). pythonTracebackFramePattern
//     accepts both.
//   - Short: `<path>:<line>: in <func>` — pytest's `--tb=short`
//     emits frames in this compact form, distinct from the
//     trailing `<path>:<line>: <message>` summary because of the
//     literal `in ` prefix on the function field.
//
// Frames are emitted only when at least one line matches; otherwise
// the slice stays nil so the M5 CollapseStage knows the parser had
// no structured data. Vendor is left false — the CollapseStage
// re-classifies via its pattern catalogue (site-packages, stdlib
// path).
func extractFrames(body []string) []event.StackFrame {
	var frames []event.StackFrame
	for _, line := range body {
		if m := shortTracebackFramePattern.FindStringSubmatch(strings.TrimLeft(line, " \t")); m != nil {
			ln, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			frames = append(frames, event.StackFrame{
				File:     m[1],
				Line:     ln,
				Function: m[3],
			})
			continue
		}
		if m := pythonTracebackFramePattern.FindStringSubmatch(line); m != nil {
			ln, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			frames = append(frames, event.StackFrame{
				File:     m[1],
				Line:     ln,
				Function: m[3],
			})
		}
	}
	if len(frames) == 0 {
		return nil
	}
	return frames
}

// pendingWarning is the in-flight Event for one entry under the
// `=== warnings summary ===` section.
type pendingWarning struct {
	file string
	line int
	body []string
}

// buildWarningEvent projects a pendingWarning into an event.Event
// with Severity=warn, Kind=warning. The Title is derived from the
// first line whose pattern matches a Python `*Warning: <message>`
// shape; falls back to the first non-header line, then to the
// header file:line summary.
func buildWarningEvent(p *pendingWarning) event.Event {
	var title string
	for _, line := range p.body {
		if m := warningClassLinePattern.FindStringSubmatch(line); m != nil {
			title = m[1] + ": " + m[2]
			break
		}
	}
	if title == "" && len(p.body) > 1 {
		title = strings.TrimSpace(p.body[1])
	}
	if title == "" {
		title = strings.TrimSpace(p.body[0])
	}
	var loc *event.Location
	if p.file != "" && p.line > 0 {
		loc = &event.Location{File: p.file, Line: p.line}
	}
	return event.Event{
		Severity: event.SeverityWarn,
		Kind:     "warning",
		Title:    title,
		Location: loc,
		Body:     append([]string(nil), p.body...),
	}
}

// severityWeight assigns a numeric weight to each severity for
// MinSeverity comparison. Higher weight = more severe. Mirrors
// the generic format's helper so the precedence rules match.
func severityWeight(s event.Severity) int {
	switch s {
	case event.SeverityError:
		return 3
	case event.SeverityWarn:
		return 2
	case event.SeverityInfo:
		return 1
	default:
		return 0
	}
}

// severityAtLeast reports whether got is at least as severe as
// minimum. Total over the documented Severity constants. The
// zero (empty) Severity is treated as SeverityError because every
// pytest Event the parser emits is at or above error today — the
// filter is mainly a kill-switch for the warning population.
func severityAtLeast(got, minimum event.Severity) bool {
	if minimum == "" {
		minimum = event.SeverityError
	}
	return severityWeight(got) >= severityWeight(minimum)
}

// effectiveMinSeverity reads opts and returns the minimum Severity
// the parser should emit. Mirrors the generic precedence rules:
//
//   - Zero-value MinSeverity = SeverityError (format default).
//   - KeepWarnings=true drops the floor to SeverityWarn unless
//     MinSeverity is already lower (SeverityInfo).
//   - An explicit MinSeverity ALWAYS wins over the
//     KeepWarnings=false default: setting MinSeverity=SeverityInfo
//     emits warnings even without KeepWarnings.
func effectiveMinSeverity(minSev event.Severity, keepWarnings bool) event.Severity {
	floor := minSev
	if floor == "" {
		floor = event.SeverityError
	}
	if keepWarnings && severityWeight(floor) > severityWeight(event.SeverityWarn) {
		return event.SeverityWarn
	}
	return floor
}
