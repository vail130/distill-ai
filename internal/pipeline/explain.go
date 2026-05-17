package pipeline

import (
	"context"
	"sort"
	"sync"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/tokens"
)

// ExplainLog records the drop decisions stages make during a run.
// It is goroutine-safe so multiple instrumented stages can write to
// the same log concurrently.
//
// Drops fall into four documented reason classes (per
// docs/explain.md):
//
//   - "severity-filter": the format's Parse dropped the event below
//     the requested minimum severity. Recorded by the Format itself,
//     not by any Stage. The CLI plumbs --severity through ParseOpts
//     in a follow-up; today this class is unused.
//   - "budget": BudgetStage dropped the event because it would not
//     fit under --budget. Recorded by ExplainingBudgetStage.
//   - "dedupe-evicted": derived from Event.Count > 1 at emit time;
//     the explain sink interprets a Count=K event as "K-1
//     dedupe-evicted drops". No Stage records this directly.
//   - "vendor-collapsed": derived from Event.FramesCollapsed > 0
//     at emit time; the explain sink interprets the count as the
//     number of vendor frames collapsed. No Stage records this
//     directly.
//
// The ExplainLog itself only stores the entries that need
// side-channel reporting (budget, severity-filter). The other two
// reasons are reconstructed from the emitted event's fields.
type ExplainLog struct {
	mu      sync.Mutex
	entries []ExplainEntry
}

// ExplainEntry is a single dropped-event record.
type ExplainEntry struct {
	// Reason is the documented class of drop (see ExplainLog godoc).
	Reason string
	// Title is the event's title at the moment it was dropped, so
	// the explain sink can render a useful annotation.
	Title string
	// Location, if non-nil, is the source location of the dropped
	// event. The sink renders it as "at file:line".
	Location *event.Location
	// Severity is the event's severity at the moment of drop.
	Severity event.Severity
	// Seq is a monotonically-increasing sequence number assigned by
	// Add. The explain sink sorts by Seq before emitting so the
	// output is deterministic across runs even when multiple stages
	// add entries concurrently.
	Seq int
}

// Add appends a drop entry. Thread-safe.
func (l *ExplainLog) Add(reason, title string, loc *event.Location, sev event.Severity) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, ExplainEntry{
		Reason:   reason,
		Title:    title,
		Location: loc,
		Severity: sev,
		Seq:      len(l.entries),
	})
}

// Entries returns a sorted snapshot of the drops the log has
// observed. Sorted by Seq so the order is deterministic and matches
// the order in which the stages dropped the events.
func (l *ExplainLog) Entries() []ExplainEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]ExplainEntry, len(l.entries))
	copy(out, l.entries)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Seq < out[j].Seq
	})
	return out
}

// Len returns the number of recorded drops. Thread-safe.
func (l *ExplainLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// ExplainingBudgetStage wraps a BudgetStage and records every
// dropped or truncated event to the supplied ExplainLog. The
// wrapper is constructed instead of a plain BudgetStage when
// BuildWithExplain is used; the production pipeline (Build) uses
// the bare BudgetStage so explain instrumentation imposes no cost
// on the non-explain path.
//
// Wire shape: this stage reads the upstream channel, classifies
// each event the same way BudgetStage does, but logs each drop
// through the ExplainLog before discarding it. Truncations are
// also logged (with reason "budget" and the original title) so
// the explain output reflects what the user would have gotten
// without --budget.
type ExplainingBudgetStage struct {
	// Budget is the token cap; matches BudgetStage.Budget.
	Budget int
	// Reserve is the per-run footer reserve; matches BudgetStage.Reserve.
	Reserve int
	// Estimator drives token cost calculations.
	Estimator tokens.Estimator
	// Counters is the same counters pointer the bare BudgetStage
	// would receive. Keeping them in sync means the explain sink's
	// footer can show the same aggregate stats the non-explain
	// path would.
	Counters *BudgetCounters
	// Log records each drop. Required when Budget > 0; nil for
	// Budget == 0 (the stage degrades to passthrough).
	Log *ExplainLog
}

// Run implements Stage. The implementation mirrors BudgetStage.Run
// but interposes Log.Add calls on every drop and truncation. The
// duplication is intentional: BudgetStage stays pristine for the
// non-explain path, and the explain wrapper carries the cost only
// when invoked. Helpers (severityRank, estimateEvent, truncate)
// are reused from budget.go so the cost model and severity
// ordering stay identical between the two paths.
func (s ExplainingBudgetStage) Run(ctx context.Context, in <-chan event.Event) <-chan event.Event {
	out := make(chan event.Event, cap(in))
	go func() {
		defer close(out)
		// Pass-through when Budget == 0; the BudgetStage uses the
		// same shortcut and the explain wrapper inherits it so
		// --budget=0 --explain works as a no-op pass-through.
		if s.Budget <= 0 {
			for ev := range in {
				select {
				case <-ctx.Done():
					return
				case out <- ev:
				}
			}
			return
		}
		// Buffer every event so we can sort by severity (the
		// non-streaming part of BudgetStage's contract). Each
		// event gets a sequence number so ties resolve by
		// arrival order.
		type seqEvent struct {
			seq int
			ev  event.Event
		}
		var buf []seqEvent
		for ev := range in {
			select {
			case <-ctx.Done():
				return
			default:
			}
			buf = append(buf, seqEvent{seq: len(buf), ev: ev})
			if s.Counters != nil {
				s.Counters.EventsBuffered++
			}
		}
		// Severity priority: error > warn > info; within a
		// severity, lower seq number wins (earlier arrival).
		sort.SliceStable(buf, func(i, j int) bool {
			si := severityRank(buf[i].ev.Severity)
			sj := severityRank(buf[j].ev.Severity)
			if si != sj {
				return si > sj
			}
			return buf[i].seq < buf[j].seq
		})
		reserve := s.Reserve
		if reserve <= 0 {
			reserve = DefaultBudgetReserve
		}
		remaining := s.Budget - reserve
		for i := range buf {
			if remaining <= 0 {
				if s.Counters != nil {
					s.Counters.EventsDroppedBudget++
				}
				s.Log.Add("budget", buf[i].ev.Title, buf[i].ev.Location, buf[i].ev.Severity)
				continue
			}
			ev := buf[i].ev
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
			// Try a single-line truncation if Title + Location
			// fits.
			truncated, fits := truncate(ev, s.Estimator, remaining)
			if !fits {
				if s.Counters != nil {
					s.Counters.EventsDroppedBudget++
				}
				s.Log.Add("budget", ev.Title, ev.Location, ev.Severity)
				continue
			}
			tcost := estimateEvent(s.Estimator, truncated)
			remaining -= tcost
			if s.Counters != nil {
				s.Counters.EventsEmitted++
				s.Counters.EventsTruncated++
				s.Counters.EstimatedTokens += tcost
			}
			s.Log.Add("budget", ev.Title+" [truncated]", ev.Location, ev.Severity)
			select {
			case <-ctx.Done():
				return
			case out <- truncated:
			}
		}
	}()
	return out
}

// BuildExplain returns a Pipeline wired with the explain-instrumented
// stages. The chain is identical to Build's except BudgetStage is
// replaced by ExplainingBudgetStage and the supplied ExplainLog is
// shared across the run.
//
// CollapseStage and DedupeStage are not wrapped because their
// per-event modifications (FramesCollapsed and Count) are visible
// to downstream Sinks; the explain Sink derives the drop counts
// from those fields rather than from a side channel.
func BuildExplain(src Source, sink Sink, opts Options, log *ExplainLog) (*Pipeline, error) {
	if log == nil {
		log = &ExplainLog{}
	}
	stages := []Stage{
		CollapseStage{KeepVendor: opts.KeepVendor},
		DedupeStage{Window: opts.DedupeWindow},
	}
	p := &Pipeline{
		Source:     src,
		Stages:     stages,
		Sink:       sink,
		BufferSize: opts.BufferSize,
	}
	if opts.Budget > 0 {
		est, err := tokens.ByName(opts.Tokenizer)
		if err != nil {
			return nil, err
		}
		counters := &BudgetCounters{}
		p.BudgetCounters = counters
		p.Stages = append(p.Stages, ExplainingBudgetStage{
			Budget:    opts.Budget,
			Estimator: est,
			Counters:  counters,
			Log:       log,
		})
	}
	return p, nil
}
