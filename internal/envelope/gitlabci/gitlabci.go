// Package gitlabci implements an envelope.Stripper for GitLab CI
// job logs.
//
// A GitLab CI job log decorates command output with two visible
// shapes:
//
//   - **Section envelopes**: lines matching
//     "section_start:UNIX_TIMESTAMP:section_name\r" and
//     "section_end:UNIX_TIMESTAMP:section_name\r". These mark the
//     boundaries of collapsible sections in GitLab's job-log
//     viewer. The CR terminator is a GitLab convention; the bare
//     `\n` form also appears in some runner configurations.
//   - **ANSI colour escapes** sprinkled densely through the runner
//     banner ("Running with gitlab-runner ...") and step output.
//
// At the end of every failing job the runner appends a canonical
// "ERROR: Job failed: exit code N" line. Strip translates that to
// an envelope_step_failure signal Event so consumers can route on
// it the same way GitHub Actions' "Process completed with exit code
// N." line surfaces.
//
// Unlike GitHub Actions, GitLab CI does not prefix per-line
// timestamps in the default configuration. When a project enables
// timestamping via runner config, the timestamps land in the inner
// stream and the inner format handles them — they look like
// ordinary line prefixes to gotest / pytest / jest and don't
// anchor any Event.
//
// Design references:
//   - [docs/envelope.md § Shipped strippers](../../../docs/envelope.md).
//   - GitLab "Section markers" / "Job log artifacts" runner docs
//     for the canonical command catalogue.
package gitlabci

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

// Name is the registered identifier for this stripper. Users pass
// it on the CLI via `--strip-envelope=gitlab-ci`.
const Name = "gitlab-ci"

const (
	confidenceClearMarker event.Confidence = 1.0
	confidenceFuzzy       event.Confidence = 0.8
)

// sectionPattern matches GitLab's section_start / section_end
// envelope markers. The CR terminator is optional so both
// "section_start:1700000000:build\r" and the same line without `\r`
// match.
//
// Capture groups: m[1]="start"|"end", m[2]=Unix timestamp,
// m[3]=section name (lowercase letters, digits, underscores).
var sectionPattern = regexp.MustCompile(`^section_(start|end):(\d+):([a-zA-Z0-9_]+)\r?$`)

// jobFailurePattern matches the runner's terminal "this job failed"
// line. GitLab runner emits two phrasings interchangeably across
// versions: "exit code N" (the canonical) and "exit status N" (seen
// in jobs scraped via `glab ci trace`). Both convey the same
// signal; capture either.
var jobFailurePattern = regexp.MustCompile(`^ERROR: Job failed: exit (?:code|status) (\d+)$`)

// glabPrefixPattern matches the per-line preamble `glab ci trace`
// (and `gitlab-runner` in --timestamps mode) prepends to every line:
// an RFC3339-Z timestamp, a space, a 2-digit step number, and a
// single-letter stream code (O for stdout, E for stderr). The
// terminator is either a single space (the standard case) or a
// `+` followed by an ANSI CSI "erase to end of line" sequence
// (`[0K`); glab emits the `+` framing on lines that continue an
// earlier carriage-return-terminated runner write, which is how
// every section_start: after the first one arrives.
//
// Any subsequent ANSI CSI escape sequences are also consumed by
// the same match, so the section / job-failure regexes see the
// line content directly. This matters: glab sandwiches CSI EL0
// (`[0K`) and SGR colour codes (`[31;1m`, `[36;1m`) between the
// prefix and the meaningful text on most lines, and an `^`-anchored
// section_start: or "ERROR: Job failed:" regex fails on them.
//
// Example matches (all stripped to the same suffix shape):
//
//	"2026-05-19T00:02:58.540261Z 00O section_start:..."
//	"2026-05-19T00:03:22.731006Z 00O+[0Ksection_start:..."
//	"2026-05-19T00:15:07.553120Z 00O [31;1mERROR: Job failed: ..."
var glabPrefixPattern = regexp.MustCompile("^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}\\.\\d+Z \\d{2}[A-Z](?: |\\+\x1b\\[0K)(?:\x1b\\[[0-9;]*[A-Za-z])*")

// csiPattern matches a single ANSI CSI escape sequence (the most
// common kind GitLab emits: `\x1b[31m`, `\x1b[0m`, etc.). Used by
// the fuzzy-detect heuristic to count how richly the input is
// coloured.
var csiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// fuzzyDetectHead is the byte window used by the fuzzy-detect
// heuristic. 1 KiB is enough to see GitLab's runner banner ("Running
// with gitlab-runner X.Y.Z (commit SHA)") plus a handful of CSI
// sequences.
const fuzzyDetectHead = 1024

// fuzzyDetectMinCSI is the minimum number of distinct CSI sequences
// the heuristic requires within fuzzyDetectHead before returning
// 0.8. The threshold rejects user output that happens to contain a
// stray colour escape.
const fuzzyDetectMinCSI = 5

// runnerBannerPattern matches the canonical first line GitLab CI
// emits at the top of every job log. Combined with dense ANSI, it
// is enough evidence for a fuzzy match.
var runnerBannerPattern = regexp.MustCompile(`(?m)^Running with gitlab-runner `)

// Stripper implements envelope.Stripper for GitLab CI logs. The
// zero value is the production instance; no configuration is
// exposed.
type Stripper struct{}

func init() {
	envelope.Register(Stripper{})
}

// Name returns the stable identifier "gitlab-ci".
func (Stripper) Name() string { return Name }

// Detect returns:
//
//   - 1.0 when the sample contains a "section_start:" or
//     "section_end:" envelope marker. These markers are unique to
//     GitLab CI; any presence is unambiguous evidence.
//   - 0.8 when the sample contains the "Running with gitlab-runner "
//     banner together with at least fuzzyDetectMinCSI ANSI CSI
//     sequences in the first fuzzyDetectHead bytes. This catches
//     job logs whose section markers fall outside the detection
//     window (e.g., a very long pre-script).
//   - 0.0 otherwise.
//
// Thresholds match the values documented in docs/envelope.md and
// in the M13.4 DoD.
func (Stripper) Detect(sample []byte) event.Confidence {
	if hasSectionMarker(sample) {
		return confidenceClearMarker
	}
	if hasFuzzyMatch(sample) {
		return confidenceFuzzy
	}
	return 0
}

func hasSectionMarker(sample []byte) bool {
	for _, line := range bytes.Split(sample, []byte{'\n'}) {
		// Strip the glab/gitlab-runner-timestamps prefix when
		// present so wrapped logs detect identically to raw
		// runner output. The non-wrapped case is the common
		// one and the regex is anchored, so the strip is a
		// constant-time no-op then.
		l := glabPrefixPattern.ReplaceAll(line, nil)
		if sectionPattern.Match(l) {
			return true
		}
	}
	return false
}

func hasFuzzyMatch(sample []byte) bool {
	head := sample
	if len(head) > fuzzyDetectHead {
		head = head[:fuzzyDetectHead]
	}
	if !runnerBannerPattern.Match(head) {
		return false
	}
	return len(csiPattern.FindAll(head, -1)) >= fuzzyDetectMinCSI
}

// Strip processes r line-by-line, writes the cleaned bytes to the
// returned Reader, and emits envelope-level signal Events on the
// returned channel.
//
// Transformations:
//
//   - Trailing `\r` is stripped from every line (the line endings
//     downstream encoders expect are bare `\n`).
//   - section_start / section_end lines are removed; the section
//     name is tracked so a subsequent job-failure signal can be
//     attributed to its enclosing section.
//   - The terminal "ERROR: Job failed: exit code N" line is
//     translated into one envelope_step_failure Event with
//     metadata.exit_code=N and metadata.step set to the most
//     recently-opened section when one is in scope. The line is
//     suppressed from the cleaned output.
//
// Lifecycle and cancellation match the GitHub Actions stripper:
// see internal/envelope/githubactions/githubactions.go for the
// full discussion of the scannerLoop indirection.
func (Stripper) Strip(ctx context.Context, r io.Reader) (io.Reader, <-chan event.Event, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("gitlabci: nil Reader")
	}
	pr, pw := io.Pipe()
	signals := make(chan event.Event, envelope.SignalBufferSize)
	go run(ctx, r, pw, signals)
	return pr, signals, nil
}

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

func scannerLoop(r io.Reader, lines chan<- string, errCh chan<- error) {
	defer close(lines)
	defer close(errCh)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines <- sc.Text()
	}
	if err := sc.Err(); err != nil {
		errCh <- err
	}
}

// state holds the per-Strip section context. GitLab sections do not
// nest in practice — section_end clears the slot — but we keep a
// LIFO stack for symmetry with the GitHub Actions stripper, capped
// at the same defensive maxDepth.
type state struct {
	sectionStack []string
}

const maxSectionDepth = 8

func (s *state) pushSection(name string) {
	if len(s.sectionStack) >= maxSectionDepth {
		return
	}
	s.sectionStack = append(s.sectionStack, name)
}

func (s *state) popSection(name string) {
	// Pop the matching section name from the stack. GitLab guarantees
	// well-formed section_start / section_end pairs in practice, but a
	// runtime mismatch (e.g., interleaved sections in a malformed log)
	// is handled by walking the stack from the top to the first
	// matching frame.
	for i := len(s.sectionStack) - 1; i >= 0; i-- {
		if s.sectionStack[i] == name {
			s.sectionStack = s.sectionStack[:i]
			return
		}
	}
}

func (s *state) currentSection() string {
	if len(s.sectionStack) == 0 {
		return ""
	}
	return s.sectionStack[len(s.sectionStack)-1]
}

// process applies the per-line Strip transformations.
//
// Return semantics mirror the GitHub Actions stripper's process:
//
//   - clean: line as it should appear in the cleaned output;
//     meaningful only when keep is true.
//   - signal: pointer to a synthesised Event, or nil.
//   - keep: true if the line forwards to the cleaned Reader;
//     false if the stripper consumes it.
//
// The glab/gitlab-runner-timestamps prefix is stripped first so the
// section_start / section_end / job-failure regexes apply uniformly
// to raw runner output and to glab-wrapped output. The stripped
// prefix is also dropped from the cleaned bytes so downstream format
// detection sees the same shape either way.
func process(line string, st *state) (clean string, signal *event.Event, keep bool) {
	line = strings.TrimRight(line, "\r")
	line = glabPrefixPattern.ReplaceAllString(line, "")
	if m := sectionPattern.FindStringSubmatch(line + "\r"); m != nil {
		// The regex requires `\r?$`; we just stripped the trailing
		// `\r`, so re-add it for the match. Cheaper than a second
		// regex without the CR.
		switch m[1] {
		case "start":
			st.pushSection(m[3])
		case "end":
			st.popSection(m[3])
		}
		return "", nil, false
	}
	if m := jobFailurePattern.FindStringSubmatch(line); m != nil {
		return "", buildStepFailureSignal(m[1], st.currentSection()), false
	}
	return line, nil, true
}

// buildStepFailureSignal constructs the envelope_step_failure Event
// for the canonical "Job failed" line. step is empty when no
// section is in scope; the SCHEMA.md envelope-kinds row documents
// metadata.step as optional.
func buildStepFailureSignal(exitCode, step string) *event.Event {
	ev := &event.Event{
		Severity: event.SeverityError,
		Kind:     envelope.KindEnvelopeStepFailure,
		Title:    step,
		Body:     []string{fmt.Sprintf("Job failed: exit code %s", exitCode)},
		Metadata: map[string]string{
			"exit_code": exitCode,
		},
	}
	if step != "" {
		ev.Metadata["step"] = step
	}
	return ev
}
