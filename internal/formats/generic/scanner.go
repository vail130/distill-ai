package generic

import (
	"bufio"
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// defaultContextLines is the number of context lines emitted before
// and after an anchor when ParseOpts.ContextLines is zero. The user
// can override via --context=N; until that wiring exists at the CLI,
// callers can set the field directly on ParseOpts.
const defaultContextLines = 3

// severityRule is one entry in the generic catalogue. The first rule
// whose pattern matches a line wins; rules are stored in priority
// order so e.g. "Traceback " (kind traceback) sorts above generic
// "Error:" (kind error_line).
type severityRule struct {
	pattern  *regexp.Regexp
	severity event.Severity
	kind     string
}

// catalogue is the v1 severity-anchor table. Patterns are compiled
// once at package init. Ordering matters: the scanner picks the
// first rule whose pattern matches the candidate line, so the more
// specific kinds (traceback, panic, exception) live above the
// generic error_line / warning_line buckets.
//
// info-level scanning is deliberately empty in v1 — healthy stdout
// has too much info noise. Future work hooks it up under an opt-in
// flag.
var catalogue = []severityRule{
	// Most specific first: traceback / panic / exception headers.
	// Trailing space on "Traceback " anchors the Python "Traceback
	// (most recent call last):" form; without it, the pattern would
	// match every line containing the word.
	{regexp.MustCompile(`\bTraceback `), event.SeverityError, "traceback"},
	{regexp.MustCompile(`^panic:`), event.SeverityError, "panic"},
	// JVM-form: `Exception in thread "main" pkg.SomeException:
	// ...` is the canonical Java stack-dump header. Followed by
	// indented `at pkg.cls.method(File.java:N)` frames, it
	// behaves as a traceback rather than a one-line exception.
	{regexp.MustCompile(`^Exception in thread`), event.SeverityError, "traceback"},
	// Inline `pkg.SomeException:` matched elsewhere in a line
	// (no `at` stack following) — single-line exception event.
	{regexp.MustCompile(`\b[A-Z][A-Za-z0-9_]*Exception:`), event.SeverityError, "exception"},
	{regexp.MustCompile(`\bException:`), event.SeverityError, "exception"},
	// Generic error markers.
	{regexp.MustCompile(`\bERROR\b`), event.SeverityError, "error_line"},
	{regexp.MustCompile(`\bFATAL\b`), event.SeverityError, "error_line"},
	{regexp.MustCompile(`(?i)\bcaused by:`), event.SeverityError, "error_line"},
	{regexp.MustCompile(`\bError:`), event.SeverityError, "error_line"},
	// Warnings come last so the more specific error rules win when
	// a single line contains both (rare but documented).
	{regexp.MustCompile(`\bWARN(?:ING)?\b`), event.SeverityWarn, "warning_line"},
	{regexp.MustCompile(`\bDeprecation\b`), event.SeverityWarn, "warning_line"},
	{regexp.MustCompile(`^W\d{4}:`), event.SeverityWarn, "warning_line"},
	{regexp.MustCompile(`\bWarning:`), event.SeverityWarn, "warning_line"},
}

// ansiEscape matches a single ANSI Select Graphic Rendition escape
// sequence (the colour codes typical command-line tools emit). The
// scanner strips these from Event.Title so the title is grep-able;
// Event.Body keeps the original bytes so the user sees what arrived.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// locationPattern extracts a best-effort source location from the
// anchor line: <path>:<line>(:<col>)?. The path must contain at
// least one '/' or '\' separator to keep host:port pairs from
// false-matching. Anchored with a non-word boundary on each side so
// embedded path-like markers in arbitrary log prose still match.
var locationPattern = regexp.MustCompile(`([^\s:]*[/\\][^\s:]*):(\d+)(?::(\d+))?`)

// matchCatalogue returns the first catalogue rule that matches line,
// or nil if no rule does. Encapsulated for testability.
func matchCatalogue(line string) *severityRule {
	for i := range catalogue {
		if catalogue[i].pattern.MatchString(line) {
			return &catalogue[i]
		}
	}
	return nil
}

// stripANSI returns line with every ANSI SGR escape removed. Used
// only for Event.Title; Event.Body keeps the original.
func stripANSI(line string) string {
	if !strings.Contains(line, "\x1b[") {
		return line
	}
	return ansiEscape.ReplaceAllString(line, "")
}

// extractLocation parses the best-effort path:line(:col)? from the
// anchor line. Returns nil when no valid path:line is found.
func extractLocation(line string) *event.Location {
	m := locationPattern.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	lineNum, err := strconv.Atoi(m[2])
	if err != nil {
		return nil
	}
	loc := &event.Location{File: m[1], Line: lineNum}
	if m[3] != "" {
		col, err := strconv.Atoi(m[3])
		if err == nil {
			c := col
			loc.Column = &c
		}
	}
	return loc
}

// pending is the in-flight Event the scanner is accumulating for.
// A pending Event progresses through up to three phases:
//
//   - blockOpen: only set for traceback / panic kinds. While true,
//     subsequent lines append to ev.Body. The phase ends when a
//     line fails the kind's continuation rule, or when maxBlockLines
//     is hit, or at EOF.
//   - post-context: once the block (if any) closes, the next
//     postNeeded lines append to postContext, then merge into
//     ev.Context.
//   - emit: once post-context is full (or EOF), the Event is sent
//     and cur is cleared.
//
// At most one pending Event is held at a time.
type pending struct {
	ev          event.Event
	blockOpen   bool             // accumulating block lines into ev.Body
	contRule    continuationRule // block continuation patterns for ev.Kind
	postNeeded  int              // post-context lines still to collect
	postContext []string         // accumulated post-context
}

// parseStream runs the scanner over r and forwards Events to out.
// It is the body of Format.Parse, extracted so tests can drive it
// without going through the channel-creation boilerplate. The
// caller closes out.
//
// The scanner holds two buffers:
//
//   - preCtx: a ring of the last contextLines lines, used to
//     populate Event.Context when an anchor fires.
//   - cur: the pending Event whose trailing context is still
//     being accumulated.
//
// Memory is bounded: at most contextLines + 1 + contextLines
// strings live at any time, regardless of input size.
//
// Severity filtering happens inside the loop, not as a downstream
// Stage, because dropping a low-severity anchor line also frees
// its post-context window. The filtered line stays in preCtx so
// the next surviving Event can still include it as context.
func parseStream(ctx context.Context, r io.Reader, opts formats.ParseOpts, out chan<- event.Event) error {
	contextLines := opts.ContextLines
	if contextLines <= 0 {
		contextLines = defaultContextLines
	}
	minSeverity := effectiveMinSeverity(opts)
	scanner := bufio.NewScanner(r)
	// Allow long lines (bufio default is 64 KiB; some test outputs
	// emit longer JSON-formatted error lines). Cap at 1 MiB so
	// adversarial input can't blow the scanner buffer.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	preCtx := newRingBuffer(contextLines)
	var cur *pending
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		// Match the catalogue against an ANSI-stripped copy of
		// the line so coloured anchors still anchor; Body keeps
		// the original line for display.
		matchTarget := stripANSI(line)
		// Phase A: if we're inside an open block (traceback /
		// panic), the line either extends the block or closes it.
		// Block lines do NOT go through anchor matching — they're
		// part of the in-flight Event's Body.
		if cur != nil && cur.blockOpen {
			if cur.contRule.matches(line) {
				if len(cur.ev.Body) < maxBlockLines {
					cur.ev.Body = append(cur.ev.Body, line)
				} else if cur.ev.Body[len(cur.ev.Body)-1] != blockTruncatedSentinel {
					cur.ev.Body[len(cur.ev.Body)-1] = blockTruncatedSentinel
				}
				preCtx.push(line)
				continue
			}
			// Block terminator: finalise the block and fall
			// through so this line still gets the post-context
			// / anchor treatment below.
			finaliseBlock(cur)
		}
		rule := matchCatalogue(matchTarget)
		// Phase B: if we have an in-flight Event (block closed
		// or never open), this line either completes its
		// post-context window or contributes one more line.
		if cur != nil && !cur.blockOpen {
			cur.postContext = append(cur.postContext, line)
			cur.postNeeded--
			if cur.postNeeded == 0 {
				cur.ev.Context = append(cur.ev.Context, cur.postContext...)
				if err := send(ctx, out, cur.ev); err != nil {
					return err
				}
				cur = nil
			}
		}
		// Phase C: the current line may itself be a new anchor.
		// The DoD says "lines that themselves match the severity
		// catalogue are still included as context — the scanner
		// does not deduplicate adjacent matches into a single
		// Event." If a previous Event is still pending, flush it
		// first with whatever post-context it managed to gather.
		if rule != nil && severityAtLeast(rule.severity, minSeverity) {
			if cur != nil {
				if len(cur.postContext) > 0 {
					cur.ev.Context = append(cur.ev.Context, cur.postContext...)
				}
				if err := send(ctx, out, cur.ev); err != nil {
					return err
				}
				cur = nil
			}
			ev := buildEvent(line, matchTarget, rule, preCtx.snapshot())
			cur = &pending{ev: ev, postNeeded: contextLines, postContext: make([]string, 0, contextLines)}
			if contRule, ok := continuationFor(rule.kind); ok {
				cur.blockOpen = true
				cur.contRule = contRule
			}
			if !cur.blockOpen && contextLines == 0 {
				// No block, no post-context wanted; emit now.
				if err := send(ctx, out, cur.ev); err != nil {
					return err
				}
				cur = nil
			}
		}
		// Phase D: the line slides into the pre-context ring
		// for future anchors.
		preCtx.push(line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	// EOF: flush any pending Event. If we were still inside a
	// block, finalise it; then append whatever post-context we
	// managed to gather.
	if cur != nil {
		if cur.blockOpen {
			finaliseBlock(cur)
		}
		if len(cur.postContext) > 0 {
			cur.ev.Context = append(cur.ev.Context, cur.postContext...)
		}
		if err := send(ctx, out, cur.ev); err != nil {
			return err
		}
	}
	return nil
}

// finaliseBlock closes an open traceback / panic block: extracts
// stack frames from the accumulated Body, re-derives the Title for
// traceback Events (last non-blank Body line carries the actual
// exception message), and clears blockOpen so the post-context
// phase can begin.
func finaliseBlock(p *pending) {
	p.blockOpen = false
	switch p.ev.Kind {
	case "traceback":
		p.ev.Frames = extractTracebackFrames(p.ev.Body)
		if t := lastNonBlank(p.ev.Body); t != "" {
			p.ev.Title = strings.TrimRight(stripANSI(t), " \t\r")
		}
	case "panic":
		p.ev.Frames = extractGoPanicFrames(p.ev.Body)
		// Panic Title stays as the original `panic: <message>`
		// line per the DoD.
	}
}

// buildEvent shapes an Event from an anchor line, the ANSI-stripped
// matchTarget (for Title), its matched rule, and the pre-context
// snapshot. Body keeps the original line so the user sees what was
// emitted; Title carries the ANSI-stripped form for grep-ability.
func buildEvent(line, matchTarget string, rule *severityRule, preCtx []string) event.Event {
	title := strings.TrimRight(matchTarget, " \t\r")
	ev := event.Event{
		Severity: rule.severity,
		Kind:     rule.kind,
		Title:    title,
		Body:     []string{line},
		Location: extractLocation(matchTarget),
	}
	if len(preCtx) > 0 {
		ev.Context = append([]string(nil), preCtx...)
	}
	return ev
}

// send forwards ev to out, honouring ctx so cancellation
// propagates cleanly even when a downstream stage is slow.
func send(ctx context.Context, out chan<- event.Event, ev event.Event) error {
	select {
	case out <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ringBuffer is a fixed-capacity FIFO of strings. Used for the
// pre-context window. The zero value is unusable; construct via
// newRingBuffer.
type ringBuffer struct {
	buf []string
	// next is the slot the next push will write into.
	next int
	// filled is true once buf has been wrapped at least once;
	// before that, only the first `next` slots hold real data.
	filled bool
}

func newRingBuffer(size int) *ringBuffer {
	if size <= 0 {
		return &ringBuffer{}
	}
	return &ringBuffer{buf: make([]string, size)}
}

func (r *ringBuffer) push(s string) {
	if len(r.buf) == 0 {
		return
	}
	r.buf[r.next] = s
	r.next++
	if r.next == len(r.buf) {
		r.next = 0
		r.filled = true
	}
}

// severityWeight assigns a numeric weight to each severity for the
// MinSeverity comparison. Higher weight = more severe.
func severityWeight(s event.Severity) int {
	switch s {
	case event.SeverityError:
		return 3
	case event.SeverityWarn:
		return 2
	case event.SeverityInfo:
		return 1
	default:
		// Unknown severities sort below info so a parser that
		// emits something unexpected doesn't accidentally pass
		// every filter.
		return 0
	}
}

// severityAtLeast reports whether got is at least as severe as
// minimum. Total over the documented Severity constants.
func severityAtLeast(got, minimum event.Severity) bool {
	return severityWeight(got) >= severityWeight(minimum)
}

// effectiveMinSeverity reads opts and returns the minimum Severity
// the parser should emit. Rules:
//
//   - Zero-value MinSeverity is treated as SeverityError (the
//     format-default for generic).
//   - KeepWarnings=true drops the effective minimum to SeverityWarn
//     unless MinSeverity is already lower (e.g. SeverityInfo).
//   - An explicit MinSeverity ALWAYS wins over the KeepWarnings=false
//     default: setting MinSeverity=SeverityInfo emits warnings even
//     without KeepWarnings.
func effectiveMinSeverity(opts formats.ParseOpts) event.Severity {
	floor := opts.MinSeverity
	if floor == "" {
		floor = event.SeverityError
	}
	if opts.KeepWarnings && severityWeight(floor) > severityWeight(event.SeverityWarn) {
		return event.SeverityWarn
	}
	return floor
}

// snapshot returns the ring's contents in chronological order
// (oldest to newest), copied out so the caller can hold the slice
// independently of subsequent pushes.
func (r *ringBuffer) snapshot() []string {
	if len(r.buf) == 0 {
		return nil
	}
	if !r.filled {
		out := make([]string, r.next)
		copy(out, r.buf[:r.next])
		return out
	}
	out := make([]string, len(r.buf))
	copy(out, r.buf[r.next:])
	copy(out[len(r.buf)-r.next:], r.buf[:r.next])
	return out
}
