package event

import (
	"reflect"
	"testing"
)

func evt(title, file string, line int) Event {
	if file == "" {
		return Event{Title: title}
	}
	return Event{
		Title:    title,
		Location: &Location{File: file, Line: line},
	}
}

func TestSignature_TitleAndLocationStable(t *testing.T) {
	a := evt("boom", "x.go", 10)
	b := evt("boom", "x.go", 10)
	if Signature(a) != Signature(b) {
		t.Fatalf("Signature differs for identical Events: %q vs %q", Signature(a), Signature(b))
	}
}

func TestSignature_DifferentTitlesDoNotCollide(t *testing.T) {
	a := evt("boom", "x.go", 10)
	b := evt("bang", "x.go", 10)
	if Signature(a) == Signature(b) {
		t.Fatalf("distinct Titles share Signature")
	}
}

func TestSignature_DifferentLocationsDoNotCollide(t *testing.T) {
	a := evt("boom", "x.go", 10)
	b := evt("boom", "x.go", 11)
	c := evt("boom", "y.go", 10)
	if Signature(a) == Signature(b) || Signature(a) == Signature(c) {
		t.Fatalf("Signature collapsed across distinct Locations")
	}
}

func TestSignature_NilLocationHashesAsTitleOnly(t *testing.T) {
	a := evt("only title", "", 0)
	b := evt("only title", "", 0)
	if Signature(a) != Signature(b) {
		t.Fatalf("nil-Location identical Titles differ")
	}
	c := evt("only title", "x.go", 1)
	if Signature(a) == Signature(c) {
		t.Fatalf("nil-Location Event collided with same-Title located Event")
	}
}

func TestSignature_SeparatorByteAvoidsCollision(t *testing.T) {
	// Title that ends with bytes the location string could start
	// with should not collide with a different Title+Location split
	// of the same byte sequence.
	a := Event{Title: "abc", Location: &Location{File: "x", Line: 1}}
	b := Event{Title: "abc\x00x:1", Location: nil}
	if Signature(a) == Signature(b) {
		t.Fatalf("Title boundary collided with Location bytes")
	}
}

func TestDeduper_FirstSightDoesNotEvict(t *testing.T) {
	d := NewDeduper(4)
	_, ok := d.Observe(evt("a", "f", 1))
	if ok {
		t.Fatalf("first Observe should not evict")
	}
	flushed := d.Flush()
	if len(flushed) != 1 {
		t.Fatalf("Flush() returned %d, want 1", len(flushed))
	}
	if flushed[0].Count != 1 {
		t.Fatalf("flushed Count=%d, want 1", flushed[0].Count)
	}
}

func TestDeduper_DuplicateBumpsCount(t *testing.T) {
	d := NewDeduper(4)
	for range 3 {
		if _, ok := d.Observe(evt("a", "f", 1)); ok {
			t.Fatalf("duplicate Observe unexpectedly evicted")
		}
	}
	flushed := d.Flush()
	if len(flushed) != 1 {
		t.Fatalf("Flush() returned %d, want 1", len(flushed))
	}
	if flushed[0].Count != 3 {
		t.Fatalf("Count=%d, want 3", flushed[0].Count)
	}
}

func TestDeduper_DistinctTitlesDoNotCollide(t *testing.T) {
	d := NewDeduper(4)
	d.Observe(evt("a", "f", 1))
	d.Observe(evt("b", "f", 1))
	flushed := d.Flush()
	if len(flushed) != 2 {
		t.Fatalf("Flush() returned %d, want 2", len(flushed))
	}
}

func TestDeduper_DistinctLocationsDoNotCollide(t *testing.T) {
	d := NewDeduper(4)
	d.Observe(evt("a", "f", 1))
	d.Observe(evt("a", "f", 2))
	d.Observe(evt("a", "g", 1))
	flushed := d.Flush()
	if len(flushed) != 3 {
		t.Fatalf("Flush() returned %d, want 3", len(flushed))
	}
}

func TestDeduper_NilLocationHashesAsTitleOnly(t *testing.T) {
	d := NewDeduper(4)
	d.Observe(evt("a", "", 0))
	d.Observe(evt("a", "", 0))
	flushed := d.Flush()
	if len(flushed) != 1 {
		t.Fatalf("Flush() returned %d, want 1", len(flushed))
	}
	if flushed[0].Count != 2 {
		t.Fatalf("Count=%d, want 2", flushed[0].Count)
	}
}

func TestDeduper_EvictionEmitsOldest(t *testing.T) {
	d := NewDeduper(3)
	for _, title := range []string{"a", "b", "c"} {
		if _, ok := d.Observe(evt(title, "f", 1)); ok {
			t.Fatalf("unexpected eviction within window for %q", title)
		}
	}
	evicted, ok := d.Observe(evt("d", "f", 1))
	if !ok {
		t.Fatalf("4th Observe should evict")
	}
	if evicted.Title != "a" {
		t.Fatalf("evicted Title=%q, want %q", evicted.Title, "a")
	}
	if evicted.Count != 1 {
		t.Fatalf("evicted Count=%d, want 1", evicted.Count)
	}
}

func TestDeduper_ReObserveAfterEviction(t *testing.T) {
	d := NewDeduper(2)
	d.Observe(evt("a", "f", 1))
	d.Observe(evt("a", "f", 1)) // Count=2
	d.Observe(evt("b", "f", 1))
	evicted, ok := d.Observe(evt("c", "f", 1))
	if !ok || evicted.Title != "a" {
		t.Fatalf("expected to evict a, got ok=%v title=%q", ok, evicted.Title)
	}
	// "a" re-enters as a new entry. Whether this Observe evicts
	// is incidental; what matters is the new "a" has Count=1.
	d.Observe(evt("a", "f", 1))
	flushed := d.Flush()
	var newA *Event
	for i := range flushed {
		if flushed[i].Title == "a" {
			newA = &flushed[i]
		}
	}
	if newA == nil {
		t.Fatalf("re-observed a not present in flush: %v", flushed)
	}
	if newA.Count != 1 {
		t.Fatalf("re-observed a Count=%d, want 1 (state from before eviction must not persist)", newA.Count)
	}
}

func TestDeduper_WindowZeroDisables(t *testing.T) {
	d := NewDeduper(0)
	for _, title := range []string{"a", "a", "b"} {
		evicted, ok := d.Observe(evt(title, "f", 1))
		if !ok {
			t.Fatalf("window=0: Observe(%q) should always evict-pass", title)
		}
		if evicted.Count != 1 {
			t.Fatalf("window=0 evicted Count=%d, want 1", evicted.Count)
		}
		if evicted.Title != title {
			t.Fatalf("window=0 evicted Title=%q, want %q", evicted.Title, title)
		}
	}
	if flushed := d.Flush(); len(flushed) != 0 {
		t.Fatalf("window=0 Flush() returned %d, want 0", len(flushed))
	}
}

func TestDeduper_FlushOrderOldestFirst(t *testing.T) {
	d := NewDeduper(4)
	titles := []string{"a", "b", "c"}
	for _, title := range titles {
		d.Observe(evt(title, "f", 1))
	}
	flushed := d.Flush()
	got := make([]string, len(flushed))
	for i, ev := range flushed {
		got[i] = ev.Title
	}
	if !reflect.DeepEqual(got, titles) {
		t.Fatalf("Flush order = %v, want %v (oldest first)", got, titles)
	}
}

func TestDeduper_FlushResetsState(t *testing.T) {
	d := NewDeduper(4)
	d.Observe(evt("a", "f", 1))
	d.Flush()
	if len(d.Flush()) != 0 {
		t.Fatalf("second Flush should return nothing")
	}
	// Re-observing a starts fresh.
	d.Observe(evt("a", "f", 1))
	flushed := d.Flush()
	if len(flushed) != 1 || flushed[0].Count != 1 {
		t.Fatalf("after Flush, re-Observe state did not reset: %v", flushed)
	}
}

func TestNewDeduper_NegativeWindowTreatedAsZero(t *testing.T) {
	d := NewDeduper(-5)
	evicted, ok := d.Observe(evt("a", "f", 1))
	if !ok || evicted.Title != "a" {
		t.Fatalf("negative window should behave as window=0")
	}
}
