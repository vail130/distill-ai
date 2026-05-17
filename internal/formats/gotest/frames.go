package gotest

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
)

// goPanicTailPattern matches the tab-indented `path:line +0xOFFSET`
// line that follows every function-call entry in a Go panic stack.
// The path may be absolute (`/usr/local/go/src/runtime/panic.go`) or
// module-relative (`example.com/foo/bar.go`); we require a `.go`
// suffix to keep host:port-style noise from matching. The +0x
// offset is optional; some frames (created-by lines) omit it.
var goPanicTailPattern = regexp.MustCompile(`^\s+(\S+\.go):(\d+)(?: \+0x[0-9a-f]+)?$`)

// goFuncCallPattern matches the function-with-args line that
// precedes a tail entry. Captures the function name (everything up
// to the `(`). Real gotest output uses dots, slashes, asterisks,
// parentheses, brackets, and `[...]` for type-parameter
// instantiations.
var goFuncCallPattern = regexp.MustCompile(`^([\w./*()\[\]-]+)\(`)

// extractGoFrames walks body and pulls out structured stack frames.
// Each frame is the pair: a function-call line followed by an
// indented `\tpath:line +0xOFFSET` tail. Returns the frames in
// source order. Vendor stays false — internal/event/collapse.go's
// ClassifyFrames re-populates Vendor at the pipeline stage.
//
// The walk is forgiving: lines that don't fit either shape are
// skipped silently. This handles `[signal SIGSEGV: ...]` headers,
// `goroutine N [running]:` headers, blank separators, and the
// `created by ...` lines that sit above a final tail entry.
func extractGoFrames(body []string) []event.StackFrame {
	var frames []event.StackFrame
	for i, line := range body {
		m := goPanicTailPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ln, err := strconv.Atoi(m[2])
		if err != nil || ln <= 0 {
			continue
		}
		fn := ""
		if i > 0 {
			fn = goFuncFromCallLine(body[i-1])
		}
		frames = append(frames, event.StackFrame{
			File:     m[1],
			Line:     ln,
			Function: fn,
		})
	}
	return frames
}

// goFuncFromCallLine extracts the function name from a
// `pkg.Func(args)` line by trimming everything from the `(`
// onward. Handles `created by pkg.Func` and indented variants by
// trimming leading whitespace and the `created by ` prefix.
func goFuncFromCallLine(line string) string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "created by ")
	if m := goFuncCallPattern.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}
