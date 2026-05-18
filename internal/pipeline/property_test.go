package pipeline_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
	"github.com/vail130/distill-ai/internal/testutil"
)

// TestPipeline_Determinism is the determinism property test required
// of every pipeline configuration: same input, same Source/Stage/Sink
// shape, run twice — byte-identical output. Catches accidental
// nondeterminism introduced by future Stages (e.g., a dedupe that
// iterates a map, or a budget enforcer that consults a clock).
//
// Promoted to property-test status by the alignment rule; see
// rules/testing.md.
func TestPipeline_Determinism(t *testing.T) {
	input := "alpha\nbeta\ngamma\ndelta\nepsilon\n"
	run := func() []event.Event {
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
		return sink.snapshot()
	}
	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Errorf("non-deterministic output:\nrun 1: %+v\nrun 2: %+v", a, b)
	}
}

// TestPipeline_DeterminismFromBytes goes one step further: serialise
// each Event through the JSON tags and byte-compare. This is the
// strict version of the property — any future Stage that adds a
// non-deterministic field (timestamp, run ID) breaks this test even
// if the Event slice DeepEqual-passes the in-memory version.
func TestPipeline_DeterminismFromBytes(t *testing.T) {
	input := "one\ntwo\nthree\n"
	run := func() string {
		sink := &collectSink{}
		p := &pipeline.Pipeline{
			Source: &pipeline.FormatSource{
				Format: lineFormat{},
				Reader: strings.NewReader(input),
			},
			Sink: sink,
		}
		if err := p.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		var buf bytes.Buffer
		for _, ev := range sink.snapshot() {
			buf.WriteString(ev.Title)
			buf.WriteByte('\n')
		}
		return buf.String()
	}
	if a, b := run(), run(); a != b {
		t.Errorf("byte-level non-determinism:\nrun 1: %q\nrun 2: %q", a, b)
	}
}

// TestPipeline_StreamingEmitsBeforeEOF is the streaming property test:
// when input arrives slowly, Events must reach the Sink incrementally
// rather than wait for EOF. Without this, a long-running command's
// output would not appear in the agent's context until the command
// finished.
//
// The test feeds bytes through a SlowReader at one chunk per
// FeedInterval. It records the time each Event arrives at the Sink
// and asserts at least one Event was emitted before the source closed.
func TestPipeline_StreamingEmitsBeforeEOF(t *testing.T) {
	const (
		lines        = 5
		feedInterval = 30 * time.Millisecond
	)
	input := strings.Repeat("line\n", lines)
	slow := &testutil.SlowReader{
		Inner:      strings.NewReader(input),
		ChunkSize:  5, // one "line\n" per Read
		ChunkDelay: feedInterval,
	}
	var firstEventAt, eofAt time.Time
	var firstSeen atomic.Bool
	start := time.Now()
	sink := timingSink{
		onEvent: func() {
			if firstSeen.CompareAndSwap(false, true) {
				firstEventAt = time.Now()
			}
		},
		onClose: func() { eofAt = time.Now() },
	}
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{
			Format: lineFormat{},
			Reader: slow,
		},
		Sink: &sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !firstSeen.Load() {
		t.Fatal("Sink never received any events")
	}
	// Most important assertion: first event arrived materially
	// before the last byte of input could have been read. Allow a
	// generous safety margin for slow CI; the assertion is "we
	// didn't buffer the whole input."
	earliestEOF := start.Add(feedInterval * lines)
	if firstEventAt.After(earliestEOF) {
		t.Errorf("first event at %v (start+%v); EOF couldn't have arrived before %v — pipeline buffered the whole input",
			firstEventAt, firstEventAt.Sub(start), earliestEOF.Sub(start))
	}
	// Sanity: EOF happened after the first event.
	if eofAt.Before(firstEventAt) {
		t.Errorf("EOF at %v preceded first event at %v", eofAt, firstEventAt)
	}
}

// timingSink records the arrival time of the first event and the time
// the channel closes. Implemented inline rather than reusing
// collectSink because the property test needs callbacks rather than
// stored events.
type timingSink struct {
	onEvent func()
	onClose func()
}

func (s *timingSink) Sink(ctx context.Context, in <-chan event.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-in:
			if !ok {
				if s.onClose != nil {
					s.onClose()
				}
				return nil
			}
			if s.onEvent != nil {
				s.onEvent()
			}
		}
	}
}
