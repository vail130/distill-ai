package generic_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"

	// Side-effect import: register the generic Format under its
	// reserved name so formats.Get("generic") finds it.
	_ "github.com/vail130/distill-ai/internal/formats/generic"
)

// TestGeneric_RegisteredAtInit — importing the package for side
// effect registers the Format under "generic". Anchors the wiring
// future milestones depend on.
func TestGeneric_RegisteredAtInit(t *testing.T) {
	f, ok := formats.Get("generic")
	if !ok {
		t.Fatal("formats.Get(\"generic\") = false; expected the generic Format to be registered at init")
	}
	if f.Name() != "generic" {
		t.Errorf("Format.Name() = %q, want \"generic\"", f.Name())
	}
}

// TestGeneric_DetectFloorOnSeverityHit — a sample with even one
// severity-bearing line yields confidenceFloor (0.1). The exact
// value is asserted so reviewers see the magic number anchored.
func TestGeneric_DetectFloorOnSeverityHit(t *testing.T) {
	f, ok := formats.Get("generic")
	if !ok {
		t.Fatal("generic not registered")
	}
	got := f.Detect([]byte("info: starting up\nERROR: thing broke\ninfo: cleanup\n"))
	if got != 0.1 {
		t.Errorf("Detect on severity-hit sample = %v, want 0.1", got)
	}
}

// TestGeneric_DetectZeroOnNonMatch — innocuous prose with no
// severity markers returns 0.0, not the floor.
func TestGeneric_DetectZeroOnNonMatch(t *testing.T) {
	f, ok := formats.Get("generic")
	if !ok {
		t.Fatal("generic not registered")
	}
	got := f.Detect([]byte("Hello, world.\nThis is fine."))
	if got != 0 {
		t.Errorf("Detect on innocuous sample = %v, want 0", got)
	}
}

// TestGeneric_DetectBelowMinThreshold — the floor must stay below
// the detector's threshold so a specific format always wins. The
// detector enforces this externally (generic is removed from the
// candidate set up front); the test pins the constant relationship
// so a future contributor can't silently bump confidenceFloor
// above ConfidenceMinDetect.
func TestGeneric_DetectBelowMinThreshold(t *testing.T) {
	f, ok := formats.Get("generic")
	if !ok {
		t.Fatal("generic not registered")
	}
	// Use a sample that triggers the floor; assert it's below
	// ConfidenceMinDetect.
	got := f.Detect([]byte("ERROR: x"))
	if got >= event.ConfidenceMinDetect {
		t.Errorf("generic confidence on hit = %v; must stay < ConfidenceMinDetect (%v) so a specific format wins ties",
			got, event.ConfidenceMinDetect)
	}
}

// TestGeneric_ParseEmptyStub — M9.1 ships Parse as an immediately-
// closed channel so the detector's fallback path can exercise the
// new format before the scanner arrives. Once M9.2 lands this test
// becomes obsolete; the M9.2 commit replaces it with a real-content
// assertion.
func TestGeneric_ParseEmptyStub(t *testing.T) {
	f, ok := formats.Get("generic")
	if !ok {
		t.Fatal("generic not registered")
	}
	ch, err := f.Parse(context.Background(), strings.NewReader("anything\n"), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Drain. With the stub, the channel is already closed.
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("M9.1 stub Parse emitted %d Events; want 0 (M9.2 fills this in)", count)
	}
}

// TestGeneric_ParseHonoursNilReader — Parse must not panic on the
// degenerate inputs the contract permits (nil reader is not legal
// per the contract, but the stub must still return a closed channel
// for any other zero-value input).
func TestGeneric_ParseHonoursClosedChannel(t *testing.T) {
	f, ok := formats.Get("generic")
	if !ok {
		t.Fatal("generic not registered")
	}
	ch, err := f.Parse(context.Background(), strings.NewReader(""), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse(empty): %v", err)
	}
	// Reading from a closed empty channel must return immediately
	// with the zero value and ok=false.
	select {
	case _, open := <-ch:
		if open {
			t.Error("expected channel to be closed; got open with no events")
		}
	default:
		t.Error("expected closed channel to be immediately readable; got blocking receive")
	}
}
