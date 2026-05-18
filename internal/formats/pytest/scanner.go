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
func scanLoop(ctx context.Context, r io.Reader, out chan<- event.Event) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	st := stateSession
	var (
		cur            *pendingBlock
		currentSection blockKind
		seenFailures   bool
	)
	flush := func() error {
		if cur == nil {
			return nil
		}
		ev := buildEvent(cur)
		cur = nil
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
			currentSection = blockFailure
			seenFailures = true
			st = stateBlockEntry
			continue
		}
		if errorsBannerLinePattern.MatchString(line) {
			if err := flush(); err != nil {
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
			// above (e.g. `=== warnings summary ===`) terminates
			// the current section.
			if isUnknownSectionBanner(line) {
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
// stays kind-agnostic.
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
		Metadata: meta,
	}
}
