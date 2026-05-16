package pipeline

import (
	"context"
	"sort"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/tokens"
)

// DefaultBudgetReserve is the number of tokens BudgetStage holds back
// from the cap so the Sink (M7) always has room for a summary line.
// 30 tokens fits the "distilled N → M lines (T tokens) / dropped: ..."
// pair the text encoder will emit.
const DefaultBudgetReserve = 30

// BudgetTruncationSentinel is the line appended to the body of an
// Event whose body had to be shortened to fit in the remaining
// budget. Exported so encoders and tests can recognise it.
const BudgetTruncationSentinel = "... [truncated by --budget]"

// BudgetCounters records what BudgetStage observed during a run. The
// Sink (M7) reads it after Pipeline.Run returns to render the footer
// and to decide exit code 3 via ForcedDrops (M6.3).
//
// BudgetCounters is goroutine-unsafe by design: BudgetStage owns it
// while the pipeline is running and the Sink reads it only after Run
// returns. The zero value is safe to use for a pipeline without a
// BudgetStage (every counter stays zero, ForcedDrops returns false).
type BudgetCounters struct {
	// EventsBuffered is the total number of Events the stage saw on
	// its input channel.
	EventsBuffered int

	// EventsEmitted is the number of Events the stage forwarded
	// downstream (truncated events count once).
	EventsEmitted int

	// EventsDroppedBudget is the number of Events the stage dropped
	// because they did not fit in the remaining budget (and could
	// not be usefully truncated).
	EventsDroppedBudget int

	// EventsTruncated is the number of Events whose body the stage
	// shortened to fit the budget. Each truncated Event is counted
	// once in both EventsTruncated and EventsEmitted.
	EventsTruncated int

	// EstimatedTokens is the running total of estimated tokens
	// emitted, including the Reserve. The Sink can compare this
	// against the configured Budget to render the footer.
	EstimatedTokens int
}

// ForcedDrops reports whether the BudgetStage had to drop or
// truncate at least one Event to fit the budget. The CLI (M8) maps
// this to exit code 3; library callers (M14) can do the same.
// Safe to call on a nil receiver and on a zero-value BudgetCounters
// — both return false, so pipelines without a BudgetStage report
// "no forced drops" without the caller having to nil-check first.
//
// See ARCHITECTURE.md § Exit codes for the full contract.
func (c *BudgetCounters) ForcedDrops() bool {
	if c == nil {
		return false
	}
	return c.EventsDroppedBudget > 0 || c.EventsTruncated > 0
}

// BudgetStage caps the total estimated token cost of the Event stream
// at Budget. It buffers the entire input, sorts by descending
// Severity (error → warn → info) with arrival-order tie-breaking,
// then emits Events until the remaining budget would be exceeded.
// Events that do not fit are dropped; an event whose body alone
// pushes it over budget but whose Title + Location + a single body
// line fits is emitted with a truncated body and Truncated=true.
//
// BudgetStage deliberately breaks the project's streaming-first
// invariant because severity-priority ordering cannot be decided
// without seeing every Event. The tradeoff is documented in
// ARCHITECTURE.md § Budget enforcement; --budget is only meaningful
// for bounded input. When Budget == 0 the stage degrades to a
// streaming pass-through identical to PassthroughStage.
//
// Truncation eligibility uses the stage's own Estimator: an Event is
// truncatable if a single-line body plus BudgetTruncationSentinel
// fits in the remaining budget. If even the Title alone exceeds the
// remaining budget, the Event is dropped instead.
type BudgetStage struct {
	// Budget is the target token cap. Zero (or negative) disables
	// budgeting and makes the stage a streaming pass-through.
	Budget int

	// Reserve is the number of tokens to hold back for the Sink's
	// footer. Zero maps to DefaultBudgetReserve. Setting Reserve
	// higher than Budget causes the stage to emit zero events and
	// report them all as dropped.
	Reserve int

	// Estimator is the token estimator the stage uses to size each
	// Event. Required when Budget > 0; ignored when Budget == 0.
	Estimator tokens.Estimator

	// Counters, if non-nil, receives running totals. The Sink reads
	// the Counters after Pipeline.Run returns. BudgetStage owns the
	// pointer during the run; concurrent reads from the Sink while
	// the pipeline is still running are undefined.
	Counters *BudgetCounters
}

// Run implements Stage. With Budget == 0 the stage emits every
// incoming Event unchanged. Otherwise it buffers the input fully,
// orders it by descending Severity (then arrival order), and emits
// Events until the budget would be exceeded.
func (s BudgetStage) Run(ctx context.Context, in <-chan event.Event) <-chan event.Event {
	out := make(chan event.Event, cap(in))
	if s.Budget <= 0 {
		go s.passthrough(ctx, in, out)
		return out
	}
	go s.enforce(ctx, in, out)
	return out
}

// passthrough is the Budget=0 streaming path. Counters still track
// what the stage saw so the Sink can render a footer; nothing is
// dropped or truncated.
func (s BudgetStage) passthrough(ctx context.Context, in <-chan event.Event, out chan<- event.Event) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			if s.Counters != nil {
				s.Counters.EventsBuffered++
				s.Counters.EventsEmitted++
				if s.Estimator != nil {
					s.Counters.EstimatedTokens += estimateEvent(s.Estimator, ev)
				}
			}
			select {
			case <-ctx.Done():
				return
			case out <- ev:
			}
		}
	}
}

// bufferedEvent pairs an Event with the arrival sequence number used
// to break ties between same-severity entries. Sorting must be
// deterministic so the project's determinism invariant holds.
type bufferedEvent struct {
	ev  event.Event
	seq int
}

// enforce is the Budget>0 buffered path: drain input, sort, emit.
func (s BudgetStage) enforce(ctx context.Context, in <-chan event.Event, out chan<- event.Event) {
	defer close(out)
	buf := make([]bufferedEvent, 0, 64)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				s.flush(ctx, buf, out)
				return
			}
			buf = append(buf, bufferedEvent{ev: ev, seq: len(buf)})
			if s.Counters != nil {
				s.Counters.EventsBuffered++
			}
		}
	}
}

// flush sorts the buffered events by descending severity (then arrival
// order) and emits until the budget is exhausted. Counters are
// updated as it goes.
func (s BudgetStage) flush(ctx context.Context, buf []bufferedEvent, out chan<- event.Event) {
	sort.SliceStable(buf, func(i, j int) bool {
		// Higher severity first; tie-break by earlier seq.
		ri, rj := severityRank(buf[i].ev.Severity), severityRank(buf[j].ev.Severity)
		if ri != rj {
			return ri > rj
		}
		return buf[i].seq < buf[j].seq
	})
	reserve := s.Reserve
	if reserve <= 0 {
		reserve = DefaultBudgetReserve
	}
	remaining := s.Budget - reserve
	for i := range buf {
		b := buf[i]
		if remaining <= 0 {
			if s.Counters != nil {
				s.Counters.EventsDroppedBudget++
			}
			continue
		}
		ev := b.ev
		cost := estimateEvent(s.Estimator, ev)
		if cost <= remaining {
			remaining -= cost
			if s.Counters != nil {
				s.Counters.EventsEmitted++
				s.Counters.EstimatedTokens += cost
			}
			select {
			case <-ctx.Done():
				return
			case out <- ev:
			}
			continue
		}
		// Doesn't fit as-is. Can we truncate?
		truncated, fits := truncate(ev, s.Estimator, remaining)
		if !fits {
			if s.Counters != nil {
				s.Counters.EventsDroppedBudget++
			}
			continue
		}
		tcost := estimateEvent(s.Estimator, truncated)
		remaining -= tcost
		if s.Counters != nil {
			s.Counters.EventsEmitted++
			s.Counters.EventsTruncated++
			s.Counters.EstimatedTokens += tcost
		}
		select {
		case <-ctx.Done():
			return
		case out <- truncated:
		}
	}
}

// severityRank orders severities highest-cost-first for emission.
// Unknown severities sort below Info so they don't preempt errors.
func severityRank(s event.Severity) int {
	switch s {
	case event.SeverityError:
		return 3
	case event.SeverityWarn:
		return 2
	case event.SeverityInfo:
		return 1
	default:
		return 0
	}
}

// estimateEvent sizes the textual cost of an Event: Title, Location
// line, Body lines, Context lines, and the per-frame "at file:line"
// renderings. Returns 0 when est is nil (Budget=0 paths).
func estimateEvent(est tokens.Estimator, ev event.Event) int {
	if est == nil {
		return 0
	}
	n := est.Estimate(ev.Title)
	if ev.Location != nil {
		n += est.Estimate(ev.Location.File)
	}
	for _, line := range ev.Body {
		n += est.Estimate(line)
	}
	for _, line := range ev.Context {
		n += est.Estimate(line)
	}
	for _, f := range ev.Frames {
		n += est.Estimate(f.File) + est.Estimate(f.Function)
	}
	return n
}

// truncate shortens ev.Body to one line plus BudgetTruncationSentinel
// and reports whether the result fits in remaining tokens. Context,
// Frames, and FramesCollapsed are dropped from the truncated copy
// because they are auxiliary to the body. If the Title + Location
// alone exceeds remaining, fits is false.
func truncate(ev event.Event, est tokens.Estimator, remaining int) (event.Event, bool) {
	out := ev
	out.Body = nil
	out.Context = nil
	out.Frames = nil
	out.FramesCollapsed = 0
	out.Truncated = true
	if len(ev.Body) > 0 {
		out.Body = []string{ev.Body[0], BudgetTruncationSentinel}
	} else {
		out.Body = []string{BudgetTruncationSentinel}
	}
	if estimateEvent(est, out) > remaining {
		return event.Event{}, false
	}
	return out, true
}
