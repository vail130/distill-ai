package event

import (
	"container/list"
	"hash/fnv"
	"strconv"
)

// Signature returns the dedupe key for ev. Two Events share a
// Signature iff their Title and Location agree; Events with a nil
// Location hash on Title alone. The hash is FNV-64a, returned as a
// hex string so callers can use it as a map key without converting.
//
// Signature is allocation-light: it writes the input bytes through a
// streaming hasher rather than building an intermediate concatenated
// string. The separator byte 0x00 between Title and Location prevents
// a Title that ends in a digit from colliding with the start of a
// location string of a different Event.
func Signature(ev Event) string {
	h := fnv.New64a()
	if ev.Location == nil {
		// Sentinel byte for nil-Location events so a Title that
		// happens to look like the serialised "Title\x00File:Line"
		// of a located Event cannot collide with one.
		_, _ = h.Write([]byte{0x01})
		_, _ = h.Write([]byte(ev.Title))
	} else {
		_, _ = h.Write([]byte{0x02})
		_, _ = h.Write([]byte(ev.Title))
		_, _ = h.Write([]byte{0x00})
		_, _ = h.Write([]byte(ev.Location.File))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write([]byte(strconv.Itoa(ev.Location.Line)))
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// Deduper is a bounded-window collapser for Events with identical
// Signature. The first time a signature is observed the Event enters
// the LRU with Count=1; subsequent observations within the window
// bump Count instead of re-entering. Entries leave the LRU either
// because the window filled and the oldest was evicted (returned by
// Observe) or because the caller drained the remainder with Flush.
//
// Deduper is not goroutine-safe. The pipeline guarantees a single
// goroutine owns a Deduper for its lifetime.
type Deduper struct {
	window int
	order  *list.List               // *Event in insertion order, oldest at Back
	index  map[string]*list.Element // signature -> element holding *Event
}

// NewDeduper constructs a Deduper with the given window. A window of
// zero or less disables deduplication: every Observe call returns the
// input Event unchanged with Count=1, and Flush returns no entries.
//
// Callers should pick a window large enough that adjacent duplicates
// fall inside it. The default in pipeline.Options (M5.3) is documented
// there; flag wiring lands in M8.
func NewDeduper(window int) *Deduper {
	if window < 0 {
		window = 0
	}
	return &Deduper{
		window: window,
		order:  list.New(),
		index:  make(map[string]*list.Element, window),
	}
}

// Observe records ev. The returned Event and hasEvicted communicate
// what (if anything) the caller must forward downstream:
//
//   - First sight of a signature, window not yet full: the Event is
//     stored with Count=1 and hasEvicted=false.
//   - First sight of a signature, window full: the oldest entry is
//     removed from the LRU and returned as the evicted Event with
//     hasEvicted=true. The new Event takes its place with Count=1.
//   - Duplicate signature: the stored Event's Count is incremented;
//     hasEvicted=false, evicted is the zero Event.
//   - Window=0: Observe is a passthrough — it returns ev (with
//     Count=1) and hasEvicted=true, and stores nothing.
func (d *Deduper) Observe(ev Event) (evicted Event, hasEvicted bool) {
	if d.window == 0 {
		ev.Count = 1
		return ev, true
	}
	sig := Signature(ev)
	if el, ok := d.index[sig]; ok {
		stored := el.Value.(*Event)
		stored.Count++
		return Event{}, false
	}
	ev.Count = 1
	stored := ev
	el := d.order.PushFront(&stored)
	d.index[sig] = el
	if d.order.Len() > d.window {
		back := d.order.Back()
		evicted = *back.Value.(*Event)
		d.order.Remove(back)
		delete(d.index, Signature(evicted))
		return evicted, true
	}
	return Event{}, false
}

// Flush returns the remaining Events in insertion order, oldest
// first, and resets the Deduper to empty. Counts on returned Events
// reflect every Observe call made since the last Flush. Callers
// typically invoke Flush exactly once, when their input stream
// closes; further Observe calls after Flush start a fresh window.
func (d *Deduper) Flush() []Event {
	if d.order.Len() == 0 {
		return nil
	}
	out := make([]Event, 0, d.order.Len())
	for e := d.order.Back(); e != nil; e = e.Prev() {
		out = append(out, *e.Value.(*Event))
	}
	d.order.Init()
	for k := range d.index {
		delete(d.index, k)
	}
	return out
}
