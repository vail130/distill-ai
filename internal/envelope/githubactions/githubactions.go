// Package githubactions implements an envelope.Stripper for GitHub
// Actions workflow logs.
//
// A GitHub Actions log decorates the underlying command output with
// three kinds of metadata:
//
//   - **Per-line timestamps**: an RFC3339-Z prefix like
//     "2024-01-15T10:23:45.1234567Z " on every line.
//   - **Workflow commands**: lines starting with "##[group]NAME",
//     "##[endgroup]", "##[error]MSG", "##[warning]MSG",
//     "##[notice]MSG", or "##[debug]MSG". The legacy "::cmd::args"
//     form (still emitted by some actions) is handled identically.
//   - **Step-failure markers**: an "##[error]Process completed with
//     exit code N." line at the end of every failing step.
//
// The Strip method removes all three from the cleaned Reader so the
// downstream format detector sees the bare command output. Group
// markers vanish entirely; timestamps are stripped from line
// prefixes; "##[error]" / "##[warning]" / "##[notice]" lines become
// envelope signal Events with the dedicated envelope_* Kinds
// instead of polluting the cleaned stream. Step-failure markers
// become envelope_step_failure Events with the step name (recovered
// from the most recent ##[group] in scope) and exit code in
// metadata.
//
// ANSI escape sequences in the input are left untouched; every
// existing Format already strips ANSI from its Title where it
// matters, so doubling up here would only obscure debugging output.
//
// Design references:
//   - [docs/envelope.md § Shipped strippers](../../../docs/envelope.md).
//   - GitHub Actions "Workflow commands for GitHub Actions"
//     reference for the canonical command catalogue.
package githubactions

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/event"
)

// Name is the registered identifier for this stripper. It is the
// value users pass on the CLI via `--strip-envelope=github-actions`.
const Name = "github-actions"

const (
	confidenceClearMarker event.Confidence = 1.0
	confidenceFuzzy       event.Confidence = 0.8
)

// timestampPattern matches the leading RFC3339-Z timestamp GitHub
// Actions prepends to every line. The trailing space is part of the
// prefix; Strip uses re.ReplaceAll to remove the whole capture in one
// pass.
//
// Example match: "2024-01-15T10:23:45.1234567Z " (any number of
// fractional digits; runners commonly emit seven).
var timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z `)

// commandPattern matches both forms of GitHub Actions workflow
// commands:
//
//   - "##[cmd]args" — the "single-line" form (the dominant shape on
//     hosted runners today).
//   - "::cmd::args" — the legacy form, still emitted by older
//     actions and by `set-output` / `add-mask` lines.
//
// The capture groups expose the command name and the remaining
// argument string. Anchoring at start-of-line avoids matching
// command-like strings embedded in user output.
var commandPattern = regexp.MustCompile(`^(?:##\[(\w+)\](.*)|::(\w+)::(.*))$`)

// stepExitPattern matches the canonical "step finished failing"
// marker GitHub Actions appends after every failing step. The DoD
// requires it map to envelope_step_failure rather than a generic
// envelope_error so consumers can route on the more specific Kind.
//
// Example match: "Process completed with exit code 1."
var stepExitPattern = regexp.MustCompile(`^Process completed with exit code (\d+)\.?$`)

// maxGroupDepth caps the group nesting stack so adversarial input
// (a runaway script emitting "##[group]" forever) can't exhaust
// memory. Real workflows nest at most two or three levels; eight is
// a defensive ceiling.
const maxGroupDepth = 8

// Stripper implements envelope.Stripper for GitHub Actions logs.
// The zero value is the production-ready instance; no configuration
// is exposed.
type Stripper struct{}

func init() {
	envelope.Register(Stripper{})
}

// Name returns the stable identifier "github-actions".
func (Stripper) Name() string { return Name }

// Detect returns:
//
//   - 1.0 when the sample contains a clear workflow-command marker
//     ("##[group]", "##[error]", "##[warning]", "##[debug]",
//     "##[notice]", or "::set-output ") at the start of a line.
//   - 0.8 when the sample lacks workflow commands but at least three
//     of the first ten non-blank lines carry the RFC3339-Z
//     timestamp prefix. The 3-of-10 threshold rejects user output
//     that happens to contain a single timestamp.
//   - 0.0 otherwise.
//
// The thresholds match the values documented in
// docs/envelope.md and in the M13.3 DoD. The clear-marker score
// always wins over the timestamp heuristic so a log that has both
// (the common case) is never miscategorised as the fuzzy match.
func (Stripper) Detect(sample []byte) event.Confidence {
	if hasClearMarker(sample) {
		return confidenceClearMarker
	}
	if hasTimestampHeuristic(sample) {
		return confidenceFuzzy
	}
	return 0
}

// hasClearMarker scans sample for any of the distinctive GitHub
// Actions workflow commands. Scanning is line-anchored; a "##[error]"
// embedded mid-line in user output does not match.
func hasClearMarker(sample []byte) bool {
	markers := [][]byte{
		[]byte("##[group]"),
		[]byte("##[error]"),
		[]byte("##[warning]"),
		[]byte("##[debug]"),
		[]byte("##[notice]"),
		[]byte("::set-output "),
	}
	for _, line := range bytes.Split(sample, []byte{'\n'}) {
		// Skip the leading timestamp prefix if present so a
		// timestamped "##[error]" still matches.
		l := timestampPattern.ReplaceAll(line, nil)
		for _, m := range markers {
			if bytes.HasPrefix(l, m) {
				return true
			}
		}
	}
	return false
}

// hasTimestampHeuristic returns true when at least three of the
// first ten non-blank lines of sample carry the RFC3339-Z prefix.
// This catches logs from a workflow whose actions never emit a
// workflow command yet still get timestamped by the runner.
func hasTimestampHeuristic(sample []byte) bool {
	const window = 10
	const threshold = 3
	hits := 0
	scanned := 0
	for _, line := range bytes.Split(sample, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		scanned++
		if timestampPattern.Match(line) {
			hits++
		}
		if scanned >= window {
			break
		}
	}
	return hits >= threshold
}

// Strip processes r line-by-line, writes the cleaned bytes to the
// returned Reader, and emits envelope-level signal Events on the
// returned channel.
//
// Lifecycle:
//
//   - One goroutine drives the line scanner; bytes flow from r
//     through bufio.Scanner into an internal io.Pipe writer. The
//     returned Reader is the pipe reader.
//   - Signal Events are sent on a buffered channel sized to
//     envelope.SignalBufferSize. The buffer absorbs short bursts of
//     ##[error] lines without applying backpressure to the line
//     scanner; sustained bursts apply backpressure naturally
//     because the sender blocks once the buffer fills.
//   - When r reaches EOF, the goroutine closes the pipe writer and
//     the signals channel and exits.
//   - A cancelled ctx unblocks the goroutine: it closes both
//     outputs and exits without consuming more of r. r is not
//     closed by Strip — the caller owns r's lifecycle.
//
// No full-input buffering: only the current line plus the rolling
// group-name stack live in memory at any time.
func (Stripper) Strip(ctx context.Context, r io.Reader) (io.Reader, <-chan event.Event, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("githubactions: nil Reader")
	}
	pr, pw := io.Pipe()
	signals := make(chan event.Event, envelope.SignalBufferSize)
	go run(ctx, r, pw, signals)
	return pr, signals, nil
}

// run is the strip goroutine. It owns the pipe writer and the
// signals channel; both close exactly once when the function
// returns.
//
// Cancellation note: the actual byte-reading happens in a separate
// scannerLoop goroutine because bufio.Scanner.Scan blocks in
// r.Read and Go has no portable way to cancel a blocking read on
// an arbitrary io.Reader. run selects between scannerLoop's output
// channel and ctx.Done so cancellation is prompt from run's
// perspective; the scannerLoop goroutine drains naturally when r
// eventually returns. Callers that need a hard guarantee against
// scannerLoop leaks should close r when they cancel ctx — the same
// convention the rest of distill-ai uses for upstream Readers.
func run(ctx context.Context, r io.Reader, pw *io.PipeWriter, signals chan<- event.Event) {
	defer close(signals)
	defer func() { _ = pw.Close() }()
	lines := make(chan string, envelope.SignalBufferSize)
	scanErr := make(chan error, 1)
	go scannerLoop(r, lines, scanErr)
	st := &state{}
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				// Scanner finished. Propagate any error to the
				// pipe reader so a downstream detector sees it.
				if err := <-scanErr; err != nil {
					_ = pw.CloseWithError(err)
				}
				return
			}
			clean, sig, keep := process(line, st)
			if sig != nil {
				select {
				case <-ctx.Done():
					return
				case signals <- *sig:
				}
			}
			if !keep {
				continue
			}
			if _, err := io.WriteString(pw, clean); err != nil {
				return
			}
			if _, err := io.WriteString(pw, "\n"); err != nil {
				return
			}
		}
	}
}

// scannerLoop drives a bufio.Scanner over r, forwarding each line
// to lines and reporting the final scanner error on errCh. Both
// channels close exactly once. scannerLoop blocks on r.Read and
// has no ctx-cancellation mechanism of its own (see run's godoc).
func scannerLoop(r io.Reader, lines chan<- string, errCh chan<- error) {
	defer close(lines)
	defer close(errCh)
	sc := bufio.NewScanner(r)
	// Hosted runners can emit very long lines (JSON debug dumps,
	// pasted assertion diffs). Raise the scanner ceiling so a
	// single long line doesn't bsr the whole log.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines <- sc.Text()
	}
	if err := sc.Err(); err != nil {
		errCh <- err
	}
}

// state carries the mutable per-Strip context that survives across
// lines: the group-name stack used to attribute step failures.
type state struct {
	// groupStack is the LIFO of currently-open group names from
	// "##[group]NAME" markers. Bounded by maxGroupDepth.
	groupStack []string
}

func (s *state) pushGroup(name string) {
	if len(s.groupStack) >= maxGroupDepth {
		return
	}
	s.groupStack = append(s.groupStack, name)
}

func (s *state) popGroup() {
	if len(s.groupStack) == 0 {
		return
	}
	s.groupStack = s.groupStack[:len(s.groupStack)-1]
}

// currentGroup returns the deepest open group name, or "" when no
// group is in scope. Step-failure events attribute themselves to
// this name.
func (s *state) currentGroup() string {
	if len(s.groupStack) == 0 {
		return ""
	}
	return s.groupStack[len(s.groupStack)-1]
}

// process applies the three Strip transformations to a single line:
// timestamp strip, command interpretation, signal extraction.
//
// Return semantics:
//
//   - clean: the line as it should appear in the cleaned output.
//     Meaningful only when keep is true.
//   - signal: a pointer to a synthesised Event, or nil. Always
//     populated when the line is a known workflow command that
//     translates to a signal.
//   - keep: true when the line should be forwarded to the cleaned
//     Reader; false when the line is consumed by the stripper
//     (workflow commands that don't translate to signals: group /
//     endgroup / debug / set-output / etc.).
func process(line string, st *state) (clean string, signal *event.Event, keep bool) {
	stripped := timestampPattern.ReplaceAllString(line, "")
	m := commandPattern.FindStringSubmatch(stripped)
	if m == nil {
		return stripped, nil, true
	}
	cmd, args := commandFromMatch(m)
	return processCommand(cmd, args, stripped, st)
}

// commandFromMatch extracts the command name and argument string
// from a commandPattern match. Both `##[cmd]args` and `::cmd::args`
// shapes pass through here; the regex's two alternate captures land
// in different submatch positions.
func commandFromMatch(m []string) (cmd, args string) {
	// commandPattern: m[1] = ##[CMD], m[2] = args; m[3] = ::CMD::, m[4] = args.
	if m[1] != "" {
		return m[1], m[2]
	}
	return m[3], m[4]
}

// processCommand routes a recognised workflow command to its
// stripper-level behaviour. Unknown commands (a workflow command
// added to GHA after this code shipped) are treated like data: the
// original line is forwarded so the inner format can do what it
// likes with it.
func processCommand(cmd, args, original string, st *state) (string, *event.Event, bool) {
	switch cmd {
	case "group":
		st.pushGroup(args)
		return "", nil, false
	case "endgroup":
		st.popGroup()
		return "", nil, false
	case "error":
		return "", buildErrorSignal(args, st), false
	case "warning":
		return "", buildWarningSignal(args), false
	case "notice":
		return "", buildWarningSignal(args), false
	case "debug":
		// Debug lines are noise for the format detector and have
		// no useful Kind in the schema; drop them silently.
		return "", nil, false
	case "set-output", "set-env", "save-state", "add-mask", "add-path", "stop-commands", "echo":
		// Legacy command channel; drops to keep cleaned bytes
		// focused on actual command output.
		return "", nil, false
	default:
		// Unknown command — forward the original line so the
		// inner format sees it.
		return original, nil, true
	}
}

// buildErrorSignal turns an "##[error]MSG" line into an Event. The
// step-failure path takes precedence: when MSG matches
// stepExitPattern, the Event is envelope_step_failure with metadata
// rather than envelope_error.
func buildErrorSignal(msg string, st *state) *event.Event {
	msg = strings.TrimSpace(msg)
	if m := stepExitPattern.FindStringSubmatch(msg); m != nil {
		step := st.currentGroup()
		ev := &event.Event{
			Severity: event.SeverityError,
			Kind:     envelope.KindEnvelopeStepFailure,
			Title:    step,
			Body:     []string{msg},
			Metadata: map[string]string{
				"exit_code": m[1],
			},
		}
		if step != "" {
			ev.Metadata["step"] = step
		}
		return ev
	}
	return &event.Event{
		Severity: event.SeverityError,
		Kind:     envelope.KindEnvelopeError,
		Title:    msg,
		Body:     []string{msg},
	}
}

// buildWarningSignal turns an "##[warning]MSG" or "##[notice]MSG"
// line into an Event. Notices and warnings collapse to the same
// Kind because the SCHEMA.md envelope-kinds table only documents
// envelope_warning at the SeverityWarn level; notices map onto it.
func buildWarningSignal(msg string) *event.Event {
	msg = strings.TrimSpace(msg)
	return &event.Event{
		Severity: event.SeverityWarn,
		Kind:     envelope.KindEnvelopeWarning,
		Title:    msg,
		Body:     []string{msg},
	}
}
