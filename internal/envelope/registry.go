package envelope

import (
	"fmt"
	"sort"
	"sync"
)

// registry holds every Stripper registered via Register. Access is
// guarded by mu. The map is initialised lazily in Register so packages
// can safely call Register from their init() functions before this
// package's init runs (Go does not order init across packages).
var (
	mu       sync.RWMutex
	registry = map[string]Stripper{}
)

// Register adds s to the global registry under s.Name(). Intended to
// be called from a Stripper implementation's init() function so the
// binary picks the stripper up by import:
//
//	// internal/envelope/githubactions/githubactions.go
//	func init() { envelope.Register(Stripper{}) }
//
// Register panics on duplicate names, nil values, or empty Name()
// returns. These are programmer errors that should fail the binary
// on startup rather than producing surprising behaviour at runtime;
// they match the same contract as formats.Register.
//
// The reserved name "none" (envelope.ChoiceNone) cannot be registered:
// it identifies the Noop stripper, which is always available without
// going through the registry, and reusing the name for a real
// stripper would make `--strip-envelope=none` ambiguous.
func Register(s Stripper) {
	if s == nil {
		panic("envelope.Register: nil Stripper")
	}
	name := s.Name()
	if name == "" {
		panic("envelope.Register: Stripper.Name() returned empty string")
	}
	if name == ChoiceNone {
		panic(fmt.Sprintf("envelope.Register: %q is reserved for the Noop stripper", ChoiceNone))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("envelope.Register: duplicate stripper name %q", name))
	}
	registry[name] = s
}

// Get returns the Stripper registered under name and reports whether
// it exists. Lookup is case-sensitive: stripper names are lowercase
// by convention and the CLI does not normalise.
//
// Get does not return Noop for name=="none"; callers that want the
// Noop stripper construct one directly (it is a zero-value value
// type) or set Options.Choice = ChoiceNone and let Wrap handle it.
func Get(name string) (Stripper, bool) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := registry[name]
	return s, ok
}

// All returns every registered Stripper, sorted alphabetically by
// Name. The returned slice is a snapshot; callers may mutate it
// without affecting the registry. Useful for the detection fan-out
// and for tests.
//
// Noop is not included in the slice; it is constructed on demand by
// Wrap when no registered stripper claims the sample.
func All() []Stripper {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Stripper, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

// reset clears the registry. Intended for tests only; the production
// binary never deregisters. Unexported so external packages can't
// call it accidentally — they call ResetForTest below instead, which
// the testing.go helper exposes for cross-package use.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	registry = map[string]Stripper{}
}
