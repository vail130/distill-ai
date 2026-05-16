package pipeline

import (
	"context"

	"github.com/vail130/distill-ai/internal/event"
)

// DedupeStage collapses adjacent Events with identical Signature into
// a single Event with Count > 1, using an event.Deduper as the
// underlying LRU. The stage emits each Event downstream exactly once,
// either when the LRU evicts it (the window filled) or when the
// upstream channel closes (Flush). Encoders therefore see one Event
// per signature with the final Count.
//
// A zero Window disables dedupe: every Event passes through with
// Count=1 set on the way out.
//
// Stage order matters: DedupeStage must run after CollapseStage so
// signatures include the post-collapse frame layout. pipeline.Build
// (M5.3) enforces this.
type DedupeStage struct {
	// Window is the LRU capacity, in distinct signatures. Zero or
	// negative disables dedupe.
	Window int
}

// Run implements Stage. The returned channel inherits the capacity
// of in so a single BufferSize knob tunes the whole pipeline.
func (s DedupeStage) Run(ctx context.Context, in <-chan event.Event) <-chan event.Event {
	out := make(chan event.Event, cap(in))
	d := event.NewDeduper(s.Window)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-in:
				if !ok {
					flushed := d.Flush()
					for i := range flushed {
						select {
						case <-ctx.Done():
							return
						case out <- flushed[i]:
						}
					}
					return
				}
				evicted, hasEvicted := d.Observe(ev)
				if !hasEvicted {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- evicted:
				}
			}
		}
	}()
	return out
}
