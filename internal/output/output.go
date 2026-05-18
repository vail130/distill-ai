// Package output contains the Sinks that turn the pipeline's Event
// stream into bytes a user — or an LLM — can read. Three encoders ship
// in the v1 set: TextSink (compact, the default), JSONSink (schema-
// versioned JSON / ndjson, a public API), and MarkdownSink (chat-paste
// friendly). All three implement pipeline.Sink and share the same
// LineCounter helper and BudgetCounters interpretation.
//
// SchemaVersion is the single source of truth for the wire format
// version emitted by JSONSink. It is checked against docs/formats/SCHEMA.md
// by TestJSONSink_SchemaVersionMatchesDoc; any breaking change bumps
// this constant and the doc in the same commit (see the
// output-stability rule).
package output

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"github.com/vail130/distill-ai/internal/event"
)

// SchemaVersion is the JSON output schema version. Incremented for
// breaking changes per the output-stability rule. Additive changes
// (new optional fields, new enum values) do not bump this.
const SchemaVersion = 1

// LineCounter wraps an io.Reader and counts the newline-terminated
// lines that flow through it. The CLI (M8) installs one around the
// pipeline's input so the Sink can render the "distilled N → M lines"
// footer.
//
// Both the line count and the "trailing partial line" flag are stored
// atomically so a concurrent Reader (parser goroutine still running
// under context cancellation) and a Lines() caller (orchestrator
// Wait reading the Summary) can race without producing undefined
// behaviour. The trailing flag uses an int32 because Go's atomic
// package doesn't expose a Bool type that's safe across older
// toolchains; 0 means false and 1 means true.
type LineCounter struct {
	// Reader is the underlying source. Required.
	Reader io.Reader

	lines    int64
	trailing int32
}

// Read implements io.Reader and counts newlines in the bytes returned.
// A final chunk that does not end with '\n' still contributes one line
// to the count when Lines() is called after EOF (mirroring `wc -l`'s
// "count a trailing partial line" behaviour); the trailing-state flag
// resets on the next chunk that starts a new line.
func (l *LineCounter) Read(p []byte) (int, error) {
	if l.Reader == nil {
		return 0, io.ErrUnexpectedEOF
	}
	n, err := l.Reader.Read(p)
	if n > 0 {
		newlines := int64(0)
		for _, b := range p[:n] {
			if b == '\n' {
				newlines++
			}
		}
		atomic.AddInt64(&l.lines, newlines)
		if p[n-1] != '\n' {
			atomic.StoreInt32(&l.trailing, 1)
		} else {
			atomic.StoreInt32(&l.trailing, 0)
		}
	}
	return n, err
}

// Lines returns the number of newline-delimited lines observed. A
// partial trailing line (input that does not end in '\n') counts as
// one line, matching `wc -l` semantics on most platforms.
func (l *LineCounter) Lines() int {
	n := atomic.LoadInt64(&l.lines)
	if atomic.LoadInt32(&l.trailing) == 1 {
		n++
	}
	return int(n)
}

// writeCounter wraps an io.Writer and counts the lines written. Used
// internally by each Sink to populate `output_lines` in the summary.
type writeCounter struct {
	w     io.Writer
	lines int
}

func (c *writeCounter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' {
			c.lines++
		}
	}
	return c.w.Write(p)
}

// fmtCount formats an integer with thousands separators ("8,432"). The
// footers in the text / markdown encoders use it so the human-readable
// output matches the ARCHITECTURE example.
func fmtCount(n int) string {
	if n < 0 {
		return "-" + fmtCount(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// severityLabel returns the uppercase wire form of a severity, e.g.
// "ERROR". Used by text and markdown encoders for the per-event header.
func severityLabel(s event.Severity) string {
	return strings.ToUpper(s.String())
}
