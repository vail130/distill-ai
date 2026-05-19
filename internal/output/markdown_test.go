package output

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
)

func TestMarkdownSink_Goldens(t *testing.T) {
	for _, c := range loadCases(t, "markdown") {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var buf bytes.Buffer
			s := &MarkdownSink{
				Writer:        &buf,
				NoFooter:      c.NoFooter,
				FormatName:    c.FormatName,
				FenceLang:     c.FenceLang,
				InputLines:    c.InputLines,
				EstimatorName: c.EstimatorName,
			}
			feedSink(t, s, c.Events)
			goldenCompare(t, "markdown", "md", c, buf.Bytes())
		})
	}
}

func TestMarkdownSink_FenceLanguage(t *testing.T) {
	ev := simpleEvent("error", "boom")
	ev.Body = []string{"print('hi')"}
	var buf bytes.Buffer
	s := &MarkdownSink{Writer: &buf, FormatName: "pytest", FenceLang: "python", NoFooter: true}
	feedSink(t, s, []event.Event{ev})
	if !bytes.Contains(buf.Bytes(), []byte("```python\n")) {
		t.Fatalf("expected ```python fence in output:\n%s", buf.String())
	}
}

func TestMarkdownSink_NoFooter(t *testing.T) {
	ev := simpleEvent("error", "boom")
	var withFooter bytes.Buffer
	a := &MarkdownSink{Writer: &withFooter, FormatName: "pytest"}
	feedSink(t, a, []event.Event{ev})
	var noFooter bytes.Buffer
	b := &MarkdownSink{Writer: &noFooter, FormatName: "pytest", NoFooter: true}
	feedSink(t, b, []event.Event{ev})
	if !bytes.Contains(withFooter.Bytes(), []byte("---\n")) {
		t.Fatalf("default output should include ---:\n%s", withFooter.String())
	}
	if bytes.Contains(noFooter.Bytes(), []byte("\n---\n")) {
		t.Fatalf("NoFooter should suppress ---:\n%s", noFooter.String())
	}
}

func TestMarkdownSink_StreamingEmitsBeforeEOF(t *testing.T) {
	w := newProbeWriter()
	s := &MarkdownSink{Writer: w, FormatName: "pytest", NoFooter: true}
	ch := make(chan event.Event)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Sink(ctx, ch) }()
	ch <- simpleEvent("error", "first")
	// Give the goroutine time to write the first block before the
	// second event arrives.
	deadline := waitForSubstring(w, "first", 500)
	if !deadline {
		t.Fatalf("first event not visible before second; sink is buffering:\n%s", w.snapshot())
	}
	close(ch)
	<-done
}

// waitForSubstring polls w for up to ms milliseconds, returning true if
// the substring is seen. The 5ms inner sleep is fine for tests; the
// outer cap keeps the test bounded under load.
func waitForSubstring(w *probeWriter, sub string, ms int) bool {
	for i := 0; i < ms/5; i++ {
		if w.hasReceived(sub) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return w.hasReceived(sub)
}

func TestMarkdownSink_NilWriterErrors(t *testing.T) {
	s := &MarkdownSink{}
	ch := make(chan event.Event)
	close(ch)
	if err := s.Sink(context.Background(), ch); err == nil {
		t.Fatalf("expected error for nil Writer")
	}
}

func TestMarkdownSink_RendersBulletsAndFences(t *testing.T) {
	ev := simpleEvent("error", "boom")
	ev.Count = 5
	ev.FramesCollapsed = 3
	ev.Truncated = true
	ev.Body = []string{"line one", "line two"}
	ev.Context = []string{"prev1", "prev2"}
	var buf bytes.Buffer
	s := &MarkdownSink{Writer: &buf, FormatName: "pytest", NoFooter: true}
	feedSink(t, s, []event.Event{ev})
	out := buf.String()
	for _, want := range []string{
		"### [1] ERROR boom",
		"**Location:** `f.py:1`",
		"**Count:** ×5",
		"**Vendor frames collapsed:** 3",
		"**Truncated by --budget**",
		"```\nline one\nline two\n```",
		"**Context:**",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

// TestMarkdownSink_LineSourceWinsOverStaticInputLines mirrors the
// TextSink and JSONSink LineSource tests: a LineSource installed at
// Run time supersedes InputLines for footer rendering, so the CLI's
// live LineCounter is honoured.
func TestMarkdownSink_LineSourceWinsOverStaticInputLines(t *testing.T) {
	var buf bytes.Buffer
	s := &MarkdownSink{
		Writer:     &buf,
		FormatName: "pytest",
		InputLines: 7, // stale fallback
		LineSource: FixedLineSource(314),
	}
	feedSink(t, s, []event.Event{simpleEvent("error", "x")})
	out := buf.String()
	if !strings.Contains(out, "314") {
		t.Errorf("footer should include LineSource value 314, got:\n%s", out)
	}
}
