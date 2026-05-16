package formats_test

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// stubFormat is a minimal Format used only by registry tests.
type stubFormat struct{ name string }

func (s *stubFormat) Name() string                     { return s.name }
func (s *stubFormat) Detect(_ []byte) event.Confidence { return 0 }
func (s *stubFormat) Parse(_ context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	ch := make(chan event.Event)
	close(ch)
	return ch, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	f := &stubFormat{name: "alpha"}
	formats.Register(f)
	got, ok := formats.Get("alpha")
	if !ok {
		t.Fatal("Get(\"alpha\") not found after Register")
	}
	if got.Name() != "alpha" {
		t.Errorf("Get returned format with Name=%q, want %q", got.Name(), "alpha")
	}
}

func TestRegistry_GetMissingReturnsFalse(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	_, ok := formats.Get("does-not-exist")
	if ok {
		t.Error("Get on empty registry returned ok=true")
	}
}

func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&stubFormat{name: "dup"})
	defer func() {
		if r := recover(); r == nil {
			t.Error("second Register with same name did not panic")
		}
	}()
	formats.Register(&stubFormat{name: "dup"})
}

func TestRegistry_NilRegisterPanics(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register(nil) did not panic")
		}
	}()
	formats.Register(nil)
}

func TestRegistry_EmptyNameRegisterPanics(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register of format with empty Name() did not panic")
		}
	}()
	formats.Register(&stubFormat{name: ""})
}

func TestRegistry_AllIsSorted(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	// Register out of alphabetical order.
	for _, n := range []string{"gamma", "alpha", "beta"} {
		formats.Register(&stubFormat{name: n})
	}
	all := formats.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d formats; want 3", len(all))
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, w := range want {
		if all[i].Name() != w {
			t.Errorf("All()[%d].Name() = %q, want %q", i, all[i].Name(), w)
		}
	}
}

func TestRegistry_AllIsSnapshot(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&stubFormat{name: "alpha"})
	snapshot := formats.All()
	// Mutate the returned slice; must not affect the registry.
	snapshot[0] = &stubFormat{name: "MUTATED"}
	got, _ := formats.Get("alpha")
	if got.Name() != "alpha" {
		t.Errorf("mutating All() slice affected registry: Get(alpha).Name() = %q", got.Name())
	}
}

// TestRegistry_ConcurrentAccess validates that Get/All can be called
// concurrently without deadlocking or producing torn reads. The race
// detector (go test -race) catches the rest.
func TestRegistry_ConcurrentAccess(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	for _, n := range []string{"a", "b", "c", "d"} {
		formats.Register(&stubFormat{name: n})
	}
	const goroutines = 32
	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				formats.All()
				_, _ = formats.Get("a")
				_, _ = formats.Get("does-not-exist")
			}
		}()
	}
	wg.Wait()
}
