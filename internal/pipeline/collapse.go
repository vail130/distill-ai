package pipeline

import (
	"context"

	"github.com/vail130/distill-ai/internal/event"
)

// CollapseStage rebuilds each Event's Frames by classifying frames
// against event.DefaultPatterns and (when KeepVendor is false)
// removing contiguous vendor runs. FramesCollapsed is set to the
// total number of frames omitted. Events without Frames pass through
// unchanged.
//
// Stage order matters: CollapseStage must run before DedupeStage so
// dedupe signatures reflect the post-collapse frame layout.
// pipeline.Build (M5.3) enforces this.
type CollapseStage struct {
	// KeepVendor leaves vendor frames in place when true. The
	// Vendor flag is still set so encoders can style vendor frames
	// distinctly; FramesCollapsed remains zero.
	KeepVendor bool
}

// Run implements Stage. The returned channel inherits the capacity
// of in so a single BufferSize knob tunes the whole pipeline.
func (s CollapseStage) Run(ctx context.Context, in <-chan event.Event) <-chan event.Event {
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
				if len(ev.Frames) > 0 {
					collapsed, omitted := event.Collapse(ev.Frames, s.KeepVendor)
					ev.Frames = collapsed
					ev.FramesCollapsed = omitted
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
