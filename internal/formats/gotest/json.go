package gotest

import (
	"bufio"
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
)

// jsonEvent is the shape `go test -json` emits, one per line. Only
// the fields the parser cares about are unmarshalled; unknown fields
// are ignored, which keeps the parser tolerant of future gotest
// releases adding new JSON fields.
type jsonEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// scanJSONLoop parses `go test -json` output. It accumulates Output
// actions per (package, test) tuple into Body lines, then emits an
// Event on `fail`. The dispatcher in scanLoop selects this path when
// the first non-blank input line begins with `{"Time":`.
//
// The state per test is keyed by package+test so concurrent test
// execution (gotest's `-parallel` flag) interleaves cleanly. The
// number of in-flight tests is bounded by the test runner's
// parallelism; not by the input size.
func scanJSONLoop(ctx context.Context, sc *bufio.Scanner, firstLine []byte, out chan<- event.Event) error {
	type acc struct {
		body []string
	}
	pending := map[string]*acc{}
	getAcc := func(pkg, test string) *acc {
		k := pkg + "\x00" + test
		a, ok := pending[k]
		if !ok {
			a = &acc{}
			pending[k] = a
		}
		return a
	}
	isFraming := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "=== RUN"),
			strings.HasPrefix(trimmed, "=== PAUSE"),
			strings.HasPrefix(trimmed, "=== CONT"),
			strings.HasPrefix(trimmed, "--- PASS"),
			strings.HasPrefix(trimmed, "--- SKIP"),
			strings.HasPrefix(trimmed, "--- FAIL"):
			return true
		}
		return false
	}
	process := func(raw []byte) error {
		var je jsonEvent
		//nolint:nilerr // gotest sometimes emits non-JSON prose
		// around the structured stream (early build chatter,
		// race-detector framing); skip lines we can't parse rather
		// than aborting the run.
		if err := json.Unmarshal(raw, &je); err != nil {
			return nil
		}
		switch je.Action {
		case "output":
			line := strings.TrimRight(je.Output, "\n")
			if je.Test == "" {
				// Build failure: emitted with Test == "" and the
				// `path/to/file.go:line:col: msg` shape.
				m := buildErrorLinePattern.FindStringSubmatch(line)
				if m == nil {
					return nil
				}
				ln, _ := strconv.Atoi(m[2])
				col, _ := strconv.Atoi(m[3])
				pb := &pendingBuild{
					file: m[1],
					line: ln,
					col:  col,
					msg:  strings.TrimSpace(m[4]),
					pkg:  je.Package,
				}
				return sendEvent(ctx, out, finaliseBuild(pb))
			}
			if line == "" || isFraming(line) {
				return nil
			}
			getAcc(je.Package, je.Test).body = append(
				getAcc(je.Package, je.Test).body, line,
			)
		case "fail":
			if je.Test == "" {
				// Package-level fail summary: nothing to emit;
				// per-test fail actions already produced the
				// Events.
				return nil
			}
			a := getAcc(je.Package, je.Test)
			pf := &pendingFailure{
				testID: je.Test,
				pkg:    je.Package,
				body:   append([]string(nil), a.body...),
			}
			delete(pending, je.Package+"\x00"+je.Test)
			return sendEvent(ctx, out, buildFailureEvent(pf))
		case "pass", "skip":
			delete(pending, je.Package+"\x00"+je.Test)
		}
		return nil
	}
	if len(firstLine) > 0 {
		if err := process(firstLine); err != nil {
			return err
		}
	}
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		// Bytes() returns a slice that may be reused; copy
		// before unmarshalling so retained metadata strings
		// don't dangle.
		if err := process(append([]byte(nil), raw...)); err != nil {
			return err
		}
	}
	return sc.Err()
}
