package pipeline

import (
	"context"

	"github.com/vail130/distill-ai/internal/event"
)

// MaxEventsStage caps the total number of Events the pipeline emits
// to the configured limit. Once the cap is hit the stage closes its
// output channel and drains the rest of its input (without
// forwarding), allowing upstream Stages and the Source to terminate
// in their own time without blocking forever on send.
//
// Stage chain position: MaxEventsStage runs after BudgetStage in
// Build's chain order so the budget enforcer truncates and ranks
// the full stream before MaxEventsStage trims to N. A user who
// passes both --budget=N and --max-events=K gets the highest-
// severity K Events of the budget-shaped output.
//
// Limit semantics:
//
//   - Limit <= 0 disables the cap. The stage forwards every Event
//     unchanged; it is functionally equivalent to PassthroughStage
//     (chosen at Build time, not by the stage itself; Build omits
//     the stage when Limit <= 0).
//   - Limit == 1 is a valid degenerate case: exactly one Event
//     forwards, then the channel closes.
//   - The Nth Event forwards; the N+1st triggers the drain. The
//     stage never holds back the Nth Event waiting for context.
type MaxEventsStage struct {
	// Limit is the hard cap on Events that may pass through. Zero
	// or negative disables the cap; Build is expected to omit
	// the stage in that case rather than rely on the runtime
	// check.
	Limit int
}

// Run implements Stage. Closes the returned channel exactly once
// when in closes, ctx is cancelled, or the cap is hit.
func (s MaxEventsStage) Run(ctx context.Context, in <-chan event.Event) <-chan event.Event {
	out := make(chan event.Event, DefaultBufferSize)
	go func() {
		defer close(out)
		emitted := 0
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-in:
				if !ok {
					return
				}
				if s.Limit > 0 && emitted >= s.Limit {
					// Drain remaining without forwarding. The
					// drain runs synchronously so we don't leak
					// a goroutine if the upstream pipeline is
					// near completion, and so a cancelled ctx
					// short-circuits cleanly.
					drainStage(ctx, in)
					return
				}
				if !forward(ctx, out, ev) {
					return
				}
				emitted++
			}
		}
	}()
	return out
}

// drainStage reads from in until close, discarding values, with
// ctx cancellation respected. Used by MaxEventsStage to allow the
// upstream Source / Stages to terminate after the cap is reached
// without their send blocking on a closed downstream channel.
func drainStage(ctx context.Context, in <-chan event.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-in:
			if !ok {
				return
			}
		}
	}
}
