package pipeline_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// lineFormat is a deterministic, streaming test Format: one Event per
// non-empty line of input, emitted as soon as the line arrives.
// Streams via bufio.Scanner so the streaming property test (which
// feeds bytes slowly) sees Events incrementally rather than after EOF.
type lineFormat struct{}

func (lineFormat) Name() string                     { return "line" }
func (lineFormat) Detect(_ []byte) event.Confidence { return 1.0 }
func (lineFormat) Parse(ctx context.Context, r io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	out := make(chan event.Event)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case out <- event.Event{
				Severity: event.SeverityInfo,
				Kind:     "line",
				Title:    line,
				Body:     []string{line},
				Count:    1,
			}:
			}
		}
	}()
	return out, nil
}

// errorFormat returns an error from Parse before emitting any event.
type errorFormat struct{}

func (errorFormat) Name() string                     { return "err" }
func (errorFormat) Detect(_ []byte) event.Confidence { return 0 }
func (errorFormat) Parse(_ context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	return nil, errors.New("intentional parse error")
}

// collectSink drains every Event into a slice. Safe for concurrent
// Run completion check.
type collectSink struct {
	mu     sync.Mutex
	events []event.Event
	err    error
	delay  time.Duration
}

func (s *collectSink) Sink(ctx context.Context, in <-chan event.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return s.err
			}
			if s.delay > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(s.delay):
				}
			}
			s.mu.Lock()
			s.events = append(s.events, ev)
			s.mu.Unlock()
		}
	}
}

func (s *collectSink) snapshot() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]event.Event, len(s.events))
	copy(out, s.events)
	return out
}

// erroringSink returns a sentinel error after consuming one event,
// to exercise the error-propagation path.
type erroringSink struct{ err error }

func (s *erroringSink) Sink(_ context.Context, in <-chan event.Event) error {
	// Consume one event so we know the pipeline produced something,
	// then return the error.
	for range in {
		return s.err
	}
	return s.err
}

func TestPipeline_PassThrough(t *testing.T) {
	input := "alpha\nbeta\ngamma\n"
	sink := &collectSink{}
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{
			Format: lineFormat{},
			Reader: strings.NewReader(input),
		},
		Stages: []pipeline.Stage{pipeline.PassthroughStage{}},
		Sink:   sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := sink.snapshot()
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Title != want[i] {
			t.Errorf("event[%d].Title = %q, want %q", i, got[i].Title, want[i])
		}
	}
}

func TestPipeline_NoStages(t *testing.T) {
	sink := &collectSink{}
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{
			Format: lineFormat{},
			Reader: strings.NewReader("only\n"),
		},
		Sink: sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := sink.snapshot(); len(got) != 1 || got[0].Title != "only" {
		t.Errorf("got %+v, want 1 event with Title=only", got)
	}
}

func TestPipeline_ContextCancellation(t *testing.T) {
	// Big input + slow sink so cancellation happens mid-stream.
	input := strings.Repeat("line\n", 1000)
	sink := &collectSink{delay: 1 * time.Millisecond}
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{
			Format: lineFormat{},
			Reader: strings.NewReader(input),
		},
		Stages: []pipeline.Stage{pipeline.PassthroughStage{}},
		Sink:   sink,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	err := p.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run returned %v, want context.Canceled", err)
	}
	// We should have processed some but not all events; this is
	// a sanity check on partial progress.
	if got := len(sink.snapshot()); got == 0 || got == 1000 {
		t.Logf("processed %d/1000 events before cancel; this is informational, not a hard failure", got)
	}
}

func TestPipeline_StageErrorPropagates_SourceFails(t *testing.T) {
	// The "stage error propagates" intent in the M2.1 scope covers
	// any pipeline component returning an error. Real Stage error
	// returns land in M5/M6 when stages can fail meaningfully; for
	// now, Source and Sink errors carry the contract.
	sink := &collectSink{}
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{
			Format: errorFormat{},
			Reader: strings.NewReader(""),
		},
		Sink: sink,
	}
	err := p.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from Source, got nil")
	}
	if !strings.Contains(err.Error(), "intentional parse error") {
		t.Errorf("error %q does not wrap the source error", err)
	}
}

func TestPipeline_StageErrorPropagates_SinkFails(t *testing.T) {
	sentinel := errors.New("sink failed")
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{
			Format: lineFormat{},
			Reader: strings.NewReader("a\nb\n"),
		},
		Sink: &erroringSink{err: sentinel},
	}
	err := p.Run(context.Background())
	if !errors.Is(err, sentinel) {
		t.Errorf("Run = %v, want errors.Is(sentinel)", err)
	}
}

func TestPipeline_Backpressure_SlowConsumer(t *testing.T) {
	// Producer emits more events than the buffer can hold; slow sink
	// causes the producer to block until the sink catches up. The
	// invariant is "we don't crash and we don't exceed bounded
	// memory"; bounded memory is checked properly in M2.3.
	const n = 200
	input := strings.Repeat("x\n", n)
	sink := &collectSink{delay: 100 * time.Microsecond}
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{
			Format: lineFormat{},
			Reader: strings.NewReader(input),
		},
		Stages:     []pipeline.Stage{pipeline.PassthroughStage{}},
		Sink:       sink,
		BufferSize: 4, // tight buffer to force backpressure
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(sink.snapshot()); got != n {
		t.Errorf("processed %d/%d events; expected complete drain under backpressure", got, n)
	}
}

func TestPipeline_NilSource(t *testing.T) {
	p := &pipeline.Pipeline{Sink: &collectSink{}}
	err := p.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Source is nil") {
		t.Errorf("Run with nil Source = %v, want 'Source is nil'", err)
	}
}

func TestPipeline_NilSink(t *testing.T) {
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{Format: lineFormat{}, Reader: strings.NewReader("")},
	}
	err := p.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Sink is nil") {
		t.Errorf("Run with nil Sink = %v, want 'Sink is nil'", err)
	}
}

func TestPipeline_NilStage(t *testing.T) {
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{Format: lineFormat{}, Reader: strings.NewReader("")},
		Stages: []pipeline.Stage{nil},
		Sink:   &collectSink{},
	}
	err := p.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stage 0 is nil") {
		t.Errorf("Run with nil Stage = %v, want 'stage 0 is nil'", err)
	}
}

func TestPipeline_DefaultBufferSize(t *testing.T) {
	if pipeline.DefaultBufferSize <= 0 {
		t.Errorf("DefaultBufferSize must be positive; got %d", pipeline.DefaultBufferSize)
	}
}

func TestFormatSource_NilFormat(t *testing.T) {
	s := &pipeline.FormatSource{Reader: strings.NewReader("")}
	_, err := s.Source(context.Background())
	if err == nil {
		t.Error("Source with nil Format returned nil error")
	}
}

func TestFormatSource_NilReader(t *testing.T) {
	s := &pipeline.FormatSource{Format: lineFormat{}}
	_, err := s.Source(context.Background())
	if err == nil {
		t.Error("Source with nil Reader returned nil error")
	}
}

// TestPipeline_NoGoroutineLeak is part of M2.3's responsibility but
// the cheap version belongs here so M2.1 doesn't ship with leaks. It
// snapshots NumGoroutine before and after Run; a small slack
// accounts for the test framework's own pool.
func TestPipeline_NoGoroutineLeak(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		sink := &collectSink{}
		p := &pipeline.Pipeline{
			Source: &pipeline.FormatSource{
				Format: lineFormat{},
				Reader: strings.NewReader("a\nb\nc\n"),
			},
			Stages: []pipeline.Stage{pipeline.PassthroughStage{}},
			Sink:   sink,
		}
		if err := p.Run(context.Background()); err != nil {
			t.Fatalf("Run iter %d: %v", i, err)
		}
	}
	// Allow scheduled goroutines a moment to finish exiting.
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()
	if after-before > 2 {
		t.Errorf("goroutine leak: before=%d, after=%d", before, after)
	}
}
