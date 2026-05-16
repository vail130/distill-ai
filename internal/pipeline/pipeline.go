// Package pipeline wires a Source, an ordered list of Stages, and a
// Sink into a single distillation run. Each piece is a goroutine
// connected to its neighbours by a bounded channel of Events;
// backpressure is the natural consequence of bounded channels.
//
// In milestone M2 (the current state) only the skeleton ships: a
// FormatSource that wraps a formats.Format, a PassthroughStage that
// does nothing, and the Sink interface (with a stub event-counting
// implementation in the tests). The real stages — dedupe (M5), frame
// collapse (M5), budget enforcement (M6) — and the real Sinks (text /
// json / markdown encoders, M7) plug in by replacing the stub Stage /
// Sink values; the Pipeline shape stays the same.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// DefaultBufferSize is the default capacity of the channels connecting
// stages. Each channel of this size adds at most this many in-flight
// Events worth of memory. Tuned for backpressure rather than
// throughput; raise via Pipeline.BufferSize if a Stage benefits from
// batching.
const DefaultBufferSize = 16

// Source produces Events. The returned channel must be closed exactly
// once when the source is done — on EOF, context cancellation, or an
// unrecoverable error.
//
// Implementations must respect ctx: if it is cancelled before the
// source has finished, the implementation must close the channel and
// return promptly.
type Source interface {
	Source(ctx context.Context) (<-chan event.Event, error)
}

// Stage transforms a stream of Events. The Stage reads from in,
// transforms or filters Events, and writes results to a new channel
// it owns and closes when in closes (or ctx is cancelled).
//
// A no-op Stage is the identity transform; see PassthroughStage.
//
// Stage implementations must not close in; that is the previous
// stage's job. Stage implementations must close their returned channel
// exactly once.
type Stage interface {
	Run(ctx context.Context, in <-chan event.Event) <-chan event.Event
}

// Sink consumes Events. The Sink reads from in until it closes,
// writing (or counting, or whatever it does) along the way. Sink
// returns nil on clean completion or an error if writing failed or
// ctx was cancelled.
type Sink interface {
	Sink(ctx context.Context, in <-chan event.Event) error
}

// Pipeline wires a Source, an ordered list of Stages, and a Sink into
// a single run. The zero value is not usable; construct via the New*
// helpers or by populating the exported fields directly.
type Pipeline struct {
	// Source produces Events. Required.
	Source Source

	// Stages transform the Event stream in order. Empty list is
	// permitted; events go straight from Source to Sink.
	Stages []Stage

	// Sink consumes the Event stream. Required.
	Sink Sink

	// BufferSize controls the capacity of each inter-stage channel.
	// Zero (the default) is mapped to DefaultBufferSize. A small
	// positive value tightens backpressure; a larger value relaxes
	// it at the cost of in-flight memory.
	BufferSize int
}

// Run executes the pipeline end to end. It blocks until either the
// Source signals EOF and all Stages and the Sink have drained, or
// ctx is cancelled, or any component returns an error.
//
// Return-value semantics:
//
//   - nil on clean completion.
//   - ctx.Err() if ctx was cancelled before completion.
//   - The first non-nil error any component returned, with the others
//     racing-cancelled. Subsequent component errors are discarded;
//     a typical "all stages cancelled" cascade reports only the
//     originating failure.
//
// Run does not leak goroutines: every component goroutine exits
// before Run returns. Verified in M2.3 by TestPipeline_NoGoroutineLeak.
func (p *Pipeline) Run(ctx context.Context) error {
	if p.Source == nil {
		return errors.New("pipeline: Source is nil")
	}
	if p.Sink == nil {
		return errors.New("pipeline: Sink is nil")
	}
	buf := p.BufferSize
	if buf <= 0 {
		buf = DefaultBufferSize
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	g := newGroup(cancel)
	// Source feeds a relay channel sized by BufferSize. Downstream
	// stages inherit the size via cap(in), so a single BufferSize
	// knob tunes the whole pipeline's backpressure.
	raw, err := p.Source.Source(ctx)
	if err != nil {
		return fmt.Errorf("pipeline: source: %w", err)
	}
	first := make(chan event.Event, buf)
	g.Go(func() error {
		defer close(first)
		for {
			select {
			case <-ctx.Done():
				return nil
			case ev, ok := <-raw:
				if !ok {
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case first <- ev:
				}
			}
		}
	})
	// Chain stages.
	var current <-chan event.Event = first
	for i, stage := range p.Stages {
		if stage == nil {
			return fmt.Errorf("pipeline: stage %d is nil", i)
		}
		current = stage.Run(ctx, current)
	}
	// Sink consumes the tail.
	g.Go(func() error {
		if err := p.Sink.Sink(ctx, current); err != nil {
			return fmt.Errorf("pipeline: sink: %w", err)
		}
		return nil
	})
	return g.Wait()
}

// FormatSource wraps a formats.Format as a Source. It reads from r and
// hands the byte stream to f.Parse, exposing the resulting Event
// channel through the Source interface. The ParseOpts are forwarded
// to Parse unchanged.
type FormatSource struct {
	Format formats.Format
	Reader io.Reader
	Opts   formats.ParseOpts
}

// Source implements Source. Errors from Format.Parse propagate
// directly; the caller is responsible for draining whatever events
// the parser emitted before the error.
func (s *FormatSource) Source(ctx context.Context) (<-chan event.Event, error) {
	if s.Format == nil {
		return nil, errors.New("FormatSource: Format is nil")
	}
	if s.Reader == nil {
		return nil, errors.New("FormatSource: Reader is nil")
	}
	return s.Format.Parse(ctx, s.Reader, s.Opts)
}

// PassthroughStage is the identity Stage: every Event read from in is
// forwarded to out unchanged. Useful as a placeholder for stages that
// land in later milestones (M5 dedupe, M5 collapse, M6 budget). It is
// also handy in tests as a non-trivial baseline.
type PassthroughStage struct{}

// Run implements Stage.
func (PassthroughStage) Run(ctx context.Context, in <-chan event.Event) <-chan event.Event {
	out := make(chan event.Event, cap(in))
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-in:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				case out <- ev:
				}
			}
		}
	}()
	return out
}
