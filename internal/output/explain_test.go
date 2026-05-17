package output_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/output"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// TestExplainSink_KeptLine — basic case: one event in, one kept
// line out, formatted with severity + title.
func TestExplainSink_KeptLine(t *testing.T) {
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf}
	in := make(chan event.Event, 1)
	in <- event.Event{Severity: event.SeverityError, Title: "broke"}
	close(in)
	if err := sink.Sink(context.Background(), in); err != nil {
		t.Fatalf("Sink: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "kept   ERROR broke") {
		t.Errorf("output missing kept line; got %q", got)
	}
}

// TestExplainSink_DedupeAnnotation — Count=5 event produces
// "<dedupe-evicted=4>" on the kept line.
func TestExplainSink_DedupeAnnotation(t *testing.T) {
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf}
	in := make(chan event.Event, 1)
	in <- event.Event{
		Severity: event.SeverityError,
		Title:    "flaky",
		Count:    5,
	}
	close(in)
	_ = sink.Sink(context.Background(), in)
	if !strings.Contains(buf.String(), "<dedupe-evicted=4>") {
		t.Errorf("missing dedupe annotation; got %q", buf.String())
	}
}

// TestExplainSink_VendorCollapsedAnnotation — FramesCollapsed=7
// produces "<vendor-collapsed=7>" on the kept line.
func TestExplainSink_VendorCollapsedAnnotation(t *testing.T) {
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf}
	in := make(chan event.Event, 1)
	in <- event.Event{
		Severity:        event.SeverityError,
		Title:           "stack",
		FramesCollapsed: 7,
	}
	close(in)
	_ = sink.Sink(context.Background(), in)
	if !strings.Contains(buf.String(), "<vendor-collapsed=7>") {
		t.Errorf("missing vendor annotation; got %q", buf.String())
	}
}

// TestExplainSink_TruncatedAnnotation — Truncated=true adds
// "<truncated>" to the line.
func TestExplainSink_TruncatedAnnotation(t *testing.T) {
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf}
	in := make(chan event.Event, 1)
	in <- event.Event{
		Severity:  event.SeverityError,
		Title:     "trimmed",
		Truncated: true,
	}
	close(in)
	_ = sink.Sink(context.Background(), in)
	if !strings.Contains(buf.String(), "<truncated>") {
		t.Errorf("missing truncated annotation; got %q", buf.String())
	}
}

// TestExplainSink_LocationRendered — event with Location is
// rendered "at file:line".
func TestExplainSink_LocationRendered(t *testing.T) {
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf}
	in := make(chan event.Event, 1)
	in <- event.Event{
		Severity: event.SeverityError,
		Title:    "broke",
		Location: &event.Location{File: "a.go", Line: 42},
	}
	close(in)
	_ = sink.Sink(context.Background(), in)
	if !strings.Contains(buf.String(), "at a.go:42") {
		t.Errorf("missing location; got %q", buf.String())
	}
}

// TestExplainSink_DroppedLines — drops in the ExplainLog produce
// dropped:<reason> lines after the kept block.
func TestExplainSink_DroppedLines(t *testing.T) {
	log := &pipeline.ExplainLog{}
	log.Add("budget", "noisy", &event.Location{File: "x.go", Line: 1}, event.SeverityWarn)
	log.Add("budget", "more-noisy", nil, event.SeverityError)
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf, Log: log}
	in := make(chan event.Event, 1)
	in <- event.Event{Severity: event.SeverityError, Title: "kept-one"}
	close(in)
	_ = sink.Sink(context.Background(), in)
	got := buf.String()
	if !strings.Contains(got, "kept   ERROR kept-one") {
		t.Errorf("missing kept line; got %q", got)
	}
	if !strings.Contains(got, "dropped:budget WARN noisy at x.go:1") {
		t.Errorf("missing dropped budget line; got %q", got)
	}
	if !strings.Contains(got, "dropped:budget ERROR more-noisy") {
		t.Errorf("missing second dropped line; got %q", got)
	}
}

// TestExplainSink_NilLog — without an ExplainLog the sink only
// emits kept lines, no dropped block.
func TestExplainSink_NilLog(t *testing.T) {
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf, Log: nil}
	in := make(chan event.Event, 1)
	in <- event.Event{Severity: event.SeverityError, Title: "lonely"}
	close(in)
	_ = sink.Sink(context.Background(), in)
	if strings.Contains(buf.String(), "dropped:") {
		t.Errorf("nil log shouldn't produce dropped lines; got %q", buf.String())
	}
}

// TestExplainSink_NoEvents — empty input + no log → empty output,
// no error.
func TestExplainSink_NoEvents(t *testing.T) {
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf}
	in := make(chan event.Event)
	close(in)
	if err := sink.Sink(context.Background(), in); err != nil {
		t.Fatalf("Sink: %v", err)
	}
	if buf.String() != "" {
		t.Errorf("expected empty output; got %q", buf.String())
	}
}

// TestExplainSink_NilWriterErrors — Writer=nil returns an error.
func TestExplainSink_NilWriterErrors(t *testing.T) {
	sink := &output.ExplainSink{}
	in := make(chan event.Event)
	close(in)
	err := sink.Sink(context.Background(), in)
	if err == nil {
		t.Fatal("expected error for nil Writer")
	}
}

// TestExplainSink_EventsEmittedCounter — tracks kept count.
func TestExplainSink_EventsEmittedCounter(t *testing.T) {
	var buf bytes.Buffer
	sink := &output.ExplainSink{Writer: &buf}
	in := make(chan event.Event, 3)
	for i := 0; i < 3; i++ {
		in <- event.Event{Severity: event.SeverityError, Title: "x"}
	}
	close(in)
	_ = sink.Sink(context.Background(), in)
	if sink.EventsEmitted() != 3 {
		t.Errorf("EventsEmitted = %d, want 3", sink.EventsEmitted())
	}
}
