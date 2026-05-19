// Package dockercompose implements an envelope.Stripper for
// `docker compose up` and `docker compose run` output.
//
// When docker compose attaches to multiple services (or to a single
// service with replicas), the daemon prefixes every container stdout
// line with the originating service name padded to a fixed column
// and terminated with `| `:
//
//	testrunner-1  | === RUN   TestThing
//	testrunner-1  | --- FAIL: TestThing (0.01s)
//	testrunner-1  | FAIL    go.example.com/m/internal/somepkg  0.007s
//
// The padding column width is the length of the longest service-or-
// service-plus-replica name across the run; lines from a service
// whose name is shorter get extra spaces so every `|` lines up. With
// a single attached service the padding collapses to one space. The
// stripper recognises any column ≥ 1 space and peels both the prefix
// and the surrounding padding.
//
// No signal Events are synthesised. docker compose's own framing
// carries no error / warning / step-failure semantics that aren't
// already present in the inner stream (the test runner emits its
// own `FAIL:` markers; docker compose just relays bytes). The
// stripper exists purely to keep the inner-format detector from
// seeing the per-line prefix and falling back to `generic`.
//
// The stripper does NOT recognise the coloured-output form that
// recent docker compose versions emit by default
// (`\x1b[36mtestrunner-1  |\x1b[0m === RUN ...`). Users piping
// through `docker compose ... --no-ansi` or capturing the
// non-interactive stdout (the form CI runners produce) get the
// uncoloured shape this stripper handles. The coloured form is
// deferred to a follow-up; ANSI peeling would also need to
// preserve the inner test runner's own colour codes, which makes
// the regex non-trivial.
//
// Design references:
//   - [docs/envelope.md § Shipped strippers](../../../docs/envelope.md).
//   - KNOWN_ISSUES.md (pre-v1.0) issue #2, which this stripper closes.
package dockercompose

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"

	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/event"
)

// Name is the registered identifier for this stripper. Users pass
// it on the CLI via `--strip-envelope=docker-compose`.
const Name = "docker-compose"

const (
	confidenceClearMarker event.Confidence = 1.0
	confidenceFuzzy       event.Confidence = 0.8
)

// linePattern matches the docker-compose per-line prefix:
//
//	<service-name><padding-spaces>| <body>
//
// where:
//   - `<service-name>` is a docker compose service token (lowercase
//     letters, digits, underscore, dash, optional `-<replica>` digit
//     suffix). The grammar matches the docker compose project name
//     validation in the Compose spec.
//   - `<padding-spaces>` is one or more spaces. docker compose pads
//     to the longest attached service-name column so every `|`
//     lines up; when only one service is attached the padding is a
//     single space. The regex accepts both cases.
//   - `| ` is the literal separator. The trailing space is part of
//     the prefix; it is consumed so the cleaned line starts with
//     the inner program's own bytes.
//
// The pattern is anchored at start of line. A `|` appearing later in
// a body line (e.g. a shell pipe inside test output) does not match
// because the prefix must satisfy the strict service-name grammar.
var linePattern = regexp.MustCompile(`^([a-z0-9][a-z0-9_-]*(?:-\d+)?)( +)\| `)

// fuzzyMinPrefixedLines is the minimum number of distinct prefixed
// lines the fuzzy heuristic requires before scoring 0.8. The
// threshold rejects user output that happens to contain one or two
// lines that pattern-match the prefix shape — a transient command
// like `docker compose ps` summary line, or a `make -f Makefile.test
// | tee` that produced a single matching line.
const fuzzyMinPrefixedLines = 4

// Stripper implements envelope.Stripper for docker-compose container
// output. The zero value is the production instance; no
// configuration is exposed.
type Stripper struct{}

func init() {
	envelope.Register(Stripper{})
}

// Name returns the stable identifier "docker-compose".
func (Stripper) Name() string { return Name }

// Detect returns:
//
//   - 1.0 when the first non-blank line of the sample is
//     docker-compose-prefixed. Strong evidence: callers reading the
//     attached output of a single-service run see the prefix on
//     the very first byte.
//   - 0.8 when at least fuzzyMinPrefixedLines distinct lines in the
//     sample match linePattern, even if the first line does not.
//     Catches runs whose preamble (image pull progress,
//     `docker buildx build` output, apt-get install banter) runs
//     for tens of KiB before docker compose finally attaches.
//   - 0.0 otherwise.
//
// Detection scans the entire sample. Wrap's chaining loop grows the
// sample beyond the cheap 16 KiB initial window when no inner
// envelope claims it; the detector trusts whatever Wrap hands it.
// Cost is O(sample-bytes); call rate is once per Wrap iteration so
// the bytes-vs-precision tradeoff sits at the right place.
func (Stripper) Detect(sample []byte) event.Confidence {
	lines := bytes.Split(sample, []byte{'\n'})
	if firstNonBlankMatches(lines) {
		return confidenceClearMarker
	}
	if countPrefixedLines(lines) >= fuzzyMinPrefixedLines {
		return confidenceFuzzy
	}
	return 0
}

// firstNonBlankMatches returns true when the first non-blank line
// in lines carries a docker-compose prefix. Strong evidence: if the
// caller is reading the attached output of a docker compose run,
// the very first byte the runner emits already has the prefix.
func firstNonBlankMatches(lines [][]byte) bool {
	for _, l := range lines {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		return linePattern.Match(l)
	}
	return false
}

// countPrefixedLines counts how many lines in the window match the
// docker-compose prefix. Used by the fuzzy detector.
func countPrefixedLines(lines [][]byte) int {
	hits := 0
	for _, l := range lines {
		if linePattern.Match(l) {
			hits++
		}
	}
	return hits
}

// Strip peels the docker-compose prefix from every matching line. Any
// non-prefixed pre-attach preamble is dropped so format detection sees
// the attached command output at the start of the cleaned stream.
// Non-prefixed lines after attachment pass through unchanged.
//
// Lifecycle and cancellation mirror the gitlabci stripper: a
// scannerLoop goroutine drives bufio.Scanner over r and forwards
// each line to a channel; run() selects between that channel and
// ctx.Done so cancellation is prompt from the consumer's
// perspective. See internal/envelope/githubactions/githubactions.go
// for the full discussion of the indirection.
func (Stripper) Strip(ctx context.Context, r io.Reader) (io.Reader, <-chan event.Event, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("dockercompose: nil Reader")
	}
	pr, pw := io.Pipe()
	signals := make(chan event.Event)
	close(signals) // this stripper synthesises no signals.
	go run(ctx, r, pw)
	return pr, signals, nil
}

func run(ctx context.Context, r io.Reader, pw *io.PipeWriter) {
	defer func() { _ = pw.Close() }()
	lines := make(chan string, envelope.SignalBufferSize)
	scanErr := make(chan error, 1)
	go scannerLoop(r, lines, scanErr)
	attached := false
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
			clean, matched := stripPrefix(line)
			if !attached && !matched {
				continue
			}
			if matched {
				attached = true
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

// stripPrefix returns line with the docker-compose prefix removed.
func stripPrefix(line string) (string, bool) {
	loc := linePattern.FindStringIndex(line)
	if loc == nil {
		return line, false
	}
	return line[loc[1]:], true
}
