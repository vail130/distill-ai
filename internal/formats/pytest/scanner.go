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
	stateSession       state = iota // initial: before the first FAILURES banner
	stateFailureHeader              // matched FAILURES banner, awaiting `___ test_id ___`
	stateFailureBody                // accumulating a failure block's body
	stateSummary                    // post-`=== short test summary info ===`
)

// Pre-compiled regexes for the M11.2 scanner. Compiled once at
// package init; the scanner consults them per line.
var (
	// failuresBannerLinePattern matches the `=== FAILURES ===`
	// section divider at start-of-line. Width of the `=` run is
	// variable across pytest versions, hence the `=+` quantifier.
	failuresBannerLinePattern = regexp.MustCompile(`^=+\s+FAILURES\s+=+\s*$`)

	// shortSummaryBannerPattern matches the
	// `=== short test summary info ===` banner that terminates
	// the FAILURES section.
	shortSummaryBannerPattern = regexp.MustCompile(`^=+\s+short test summary info\s+=+\s*$`)

	// trailingResultPattern matches the `=== 1 failed, 2 passed
	// in 0.42s ===` final line. Also a terminator for any in-
	// flight failure body.
	trailingResultPattern = regexp.MustCompile(`^=+\s+\d+\s+(?:failed|passed|error|skipped|warning|deselected|xfailed|xpassed)`)

	// failureUnderlinePattern matches an underlined per-failure
	// header line: `___ test_id ___`. pytest emits these with at
	// least three underscores on each side. The captured group is
	// the trimmed test ID (which may contain spaces in the
	// parametrised form, e.g. `test_thing[case_a]`).
	failureUnderlinePattern = regexp.MustCompile(`^_{3,}\s+(.+?)\s+_{3,}\s*$`)

	// assertionErrorPattern matches a pytest assertion-detail line
	// (the `E   <message>` shape pytest emits beneath a `>` line
	// for long-form tracebacks). The captured group is the
	// trimmed message text.
	assertionErrorPattern = regexp.MustCompile(`^E\s+(.+?)\s*$`)

	// locationLinePattern matches a `path:line: <message>` line
	// pytest prints at the bottom of each failure block. Path
	// must contain a `/` or `\` (or end in `.py`) so unrelated
	// `host:port` references don't false-positive. Captured
	// groups are: path, line, message (optional).
	locationLinePattern = regexp.MustCompile(`^(\S+\.py|\S+[/\\]\S+):(\d+):\s*(.*)$`)
)

// parseStream runs the M11.2 scanner over r and forwards Events to
// the returned channel. Closes the channel when r reaches EOF, when
// ctx is cancelled, or when an unrecoverable I/O error occurs.
//
// Memory is bounded: the scanner holds at most one in-flight
// failure block at a time (Body grows line by line until the block
// terminates). pytest's default reporter does not emit single
// failure blocks longer than a few dozen lines in real code, so no
// hard cap is enforced at this layer.
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

// scanLoop is the state machine. Extracted from parseStream so
// tests can call it without the goroutine / channel wrapper if
// needed; today only parseStream calls it.
func scanLoop(ctx context.Context, r io.Reader, out chan<- event.Event) error {
	sc := bufio.NewScanner(r)
	// Allow long lines; pytest sometimes prints multi-KiB diffs
	// inside `E   ` assertion messages (notably from pytest-
	// assertrewrite expanding deep-equal failures). Cap at 1 MiB
	// to bound adversarial input.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	st := stateSession
	var cur *pendingFailure
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
		// The trailing result line and the short-summary banner
		// always terminate any in-flight block, regardless of
		// current state. They mark the end of the FAILURES
		// section.
		if shortSummaryBannerPattern.MatchString(line) ||
			trailingResultPattern.MatchString(line) {
			if err := flush(); err != nil {
				return err
			}
			st = stateSummary
			continue
		}
		switch st {
		case stateSession:
			// Drop everything until the FAILURES banner.
			if failuresBannerLinePattern.MatchString(line) {
				st = stateFailureHeader
			}
		case stateFailureHeader:
			// Looking for the first `___ test_id ___` underline.
			// Anything else is dropped (blank line between
			// banner and first header is common).
			if m := failureUnderlinePattern.FindStringSubmatch(line); m != nil {
				cur = &pendingFailure{
					testID:   strings.TrimSpace(m[1]),
					headerLn: line,
					body:     []string{line},
				}
				st = stateFailureBody
			}
		case stateFailureBody:
			// Another underlined header terminates the current
			// block and starts a new one.
			if m := failureUnderlinePattern.FindStringSubmatch(line); m != nil {
				if err := flush(); err != nil {
					return err
				}
				cur = &pendingFailure{
					testID:   strings.TrimSpace(m[1]),
					headerLn: line,
					body:     []string{line},
				}
				continue
			}
			// A new section banner (e.g. `=== warnings summary
			// ===`) also terminates the FAILURES section.
			if isSectionBanner(line) {
				if err := flush(); err != nil {
					return err
				}
				st = stateSummary
				continue
			}
			cur.body = append(cur.body, line)
		case stateSummary:
			// Everything past the summary banner is discarded.
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

// isSectionBanner reports whether line looks like a `=== <name>
// ===` divider. Used to detect the end of the FAILURES section
// when the trailing result line is missing (truncated input).
func isSectionBanner(line string) bool {
	if !strings.HasPrefix(line, "==") {
		return false
	}
	trimmed := strings.TrimSpace(line)
	// Must start and end with at least two `=`.
	return strings.HasPrefix(trimmed, "==") && strings.HasSuffix(trimmed, "==")
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

// pendingFailure is the in-flight Event for one failure block. The
// scanner accumulates body lines until the block terminator; at
// emit time the fields project into event.Event.
type pendingFailure struct {
	testID   string
	headerLn string
	body     []string
}

// buildFailureEvent projects the accumulated state into a final
// event.Event. The Title is the first `E   <message>` line —
// pytest's convention for the assertion / exception detail — with
// a fallback to the trimmed header. Location comes from the
// trailing `path:line:` line.
func buildFailureEvent(p *pendingFailure) event.Event {
	title := strings.TrimSpace(p.testID)
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
	// Scan from the bottom up: pytest's `path:line: <message>`
	// summary appears at or near the end of the block. Taking
	// the last match avoids picking up earlier traceback frames
	// that point at fixture or helper code.
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
	meta := map[string]string{}
	if p.testID != "" {
		meta["test_id"] = p.testID
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
