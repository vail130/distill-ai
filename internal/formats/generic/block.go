package generic

import (
	"regexp"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
)

// maxBlockLines caps how many lines a traceback / panic block may
// accumulate before the scanner terminates the block to keep memory
// bounded under adversarial input. The cap is generous: real
// tracebacks rarely exceed a few dozen frames. When the cap fires
// the final Body line is replaced with a "[block truncated]"
// sentinel so consumers can detect truncation.
const maxBlockLines = 100

// blockTruncatedSentinel marks Body when the maxBlockLines cap is
// hit. Tests pin the exact string.
const blockTruncatedSentinel = "... [block truncated]"

// continuationRule decides whether a line belongs to a traceback /
// panic block currently being accumulated. The scanner consults the
// rule by kind; nil means the line terminates the block.
type continuationRule struct {
	patterns []*regexp.Regexp
}

// matches reports whether line continues the block.
func (c continuationRule) matches(line string) bool {
	for _, p := range c.patterns {
		if p.MatchString(line) {
			return true
		}
	}
	return false
}

// tracebackContinuation describes which lines belong to a Python /
// JVM traceback block. Indented lines, blank lines, and a final
// dedented exception-message line are part of the block. A
// non-indented log line that isn't an exception message
// terminates the block.
var tracebackContinuation = continuationRule{
	patterns: []*regexp.Regexp{
		regexp.MustCompile(`^\s+File "`),          // Python frame
		regexp.MustCompile(`^\s+at `),             // JVM frame
		regexp.MustCompile(`^\s*\.{3} \d+ more$`), // JVM "N more frames" tail
		regexp.MustCompile(`^\s`),                 // any indented line
		regexp.MustCompile(`^$`),                  // blank line inside a block
		// The Python traceback terminator: a dedented line of the
		// shape "ExceptionTypeName: message" — capitalised
		// CamelCase word followed by a colon. Common types:
		// KeyError, ValueError, TypeError, AttributeError,
		// IndexError, RuntimeError, etc. Matched by structure
		// rather than by enumerating types.
		regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*(?:Error|Exception|Warning):`),
	},
}

// panicContinuation describes which lines belong to a Go panic
// block. Real Go panic stacks alternate between a non-indented
// `pkg.Func(args)` line and a tab-indented `path:line +0xOFFSET`
// tail; both shapes plus the goroutine header, blank lines, and
// repeated `panic:` entries continue the block.
var panicContinuation = continuationRule{
	patterns: []*regexp.Regexp{
		regexp.MustCompile(`^\s*goroutine \d+`),
		regexp.MustCompile(`^\s*0x[0-9a-f]+`),
		regexp.MustCompile(`^\s`), // any indented line (tab-indented tail lines, "created by" lines)
		regexp.MustCompile(`^$`),  // blank line inside a panic block
		regexp.MustCompile(`^panic: `),
		// Runtime signal subheaders: `[signal SIGSEGV: ...]`,
		// `[recovered]`, etc. Common in real Go panic output
		// between the `panic:` line and the goroutine header.
		regexp.MustCompile(`^\[`),
		// Go function-call lines: `pkg.Func(args...)` or
		// `(*T).method(args...)`. Allows alphanumerics, dots,
		// underscores, slashes (for import paths), parens, hex
		// args, and an optional `*`-receiver prefix.
		regexp.MustCompile(`^[\w./*()]+\([\w *,.]*\)$`),
	},
}

// continuationFor returns the rule for the given Event Kind, or
// (zero, false) for kinds that don't accumulate blocks.
func continuationFor(kind string) (continuationRule, bool) {
	switch kind {
	case "traceback":
		return tracebackContinuation, true
	case "panic":
		return panicContinuation, true
	default:
		return continuationRule{}, false
	}
}

// Frame extractors. Each extractor walks the accumulated Body and
// returns a slice of StackFrame in source order. Vendor stays false;
// the M5 CollapseStage's ClassifyFrames repopulates Vendor after the
// parser returns.

var (
	pythonFramePattern = regexp.MustCompile(`File "([^"]+)", line (\d+)(?:, in (\S+))?`)
	jvmFramePattern    = regexp.MustCompile(`at ([\w.$]+)\(([^):]+):(\d+)\)`)
	goPanicTailPattern = regexp.MustCompile(`^\s*(\S+):(\d+)(?: \+0x[0-9a-f]+)?$`)
)

// extractTracebackFrames pulls Python "File ..., line N, in func"
// and JVM "at func(path:line)" frames out of body in source order.
func extractTracebackFrames(body []string) []event.StackFrame {
	var frames []event.StackFrame
	for _, line := range body {
		if m := pythonFramePattern.FindStringSubmatch(line); m != nil {
			ln := atoi(m[2])
			if ln == 0 {
				continue
			}
			frames = append(frames, event.StackFrame{
				File:     m[1],
				Line:     ln,
				Function: m[3],
			})
			continue
		}
		if m := jvmFramePattern.FindStringSubmatch(line); m != nil {
			ln := atoi(m[3])
			if ln == 0 {
				continue
			}
			frames = append(frames, event.StackFrame{
				File:     m[2],
				Line:     ln,
				Function: m[1],
			})
		}
	}
	return frames
}

// extractGoPanicFrames pulls Go panic stack frames out of body. A
// Go panic stack alternates between a function-with-args line and
// a `path:line +0xOFFSET` tail line; the tail line carries the
// file/line and the function name comes from the preceding line
// with arguments stripped.
func extractGoPanicFrames(body []string) []event.StackFrame {
	var frames []event.StackFrame
	for i, line := range body {
		m := goPanicTailPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ln := atoi(m[2])
		if ln == 0 {
			continue
		}
		fn := ""
		if i > 0 {
			fn = stripCallArgs(strings.TrimSpace(body[i-1]))
		}
		frames = append(frames, event.StackFrame{
			File:     m[1],
			Line:     ln,
			Function: fn,
		})
	}
	return frames
}

// stripCallArgs trims `(args...)` off the end of a Go function
// name. The Go panic stack prints `pkg.func(0x123, 0x456)` above
// the `path:line +0xOFFSET` tail; we want just `pkg.func`.
func stripCallArgs(s string) string {
	if i := strings.IndexByte(s, '('); i > 0 {
		return s[:i]
	}
	return s
}

// atoi is a small helper for line-number parsing. Returns 0 on
// failure so callers can skip the frame.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// lastNonBlank returns the last non-blank Body line excluding the
// truncation sentinel, or empty string if every line is blank /
// the sentinel. Used to re-derive traceback Titles; the sentinel
// is structural and must not become a Title.
func lastNonBlank(body []string) string {
	for i := len(body) - 1; i >= 0; i-- {
		s := body[i]
		if strings.TrimSpace(s) == "" {
			continue
		}
		if s == blockTruncatedSentinel {
			continue
		}
		return s
	}
	return ""
}
