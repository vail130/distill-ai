package formats

import (
	"fmt"
	"sort"
	"sync"
)

// registry holds every Format registered via Register. Access is
// guarded by mu. The map is initialised lazily in Register so packages
// can safely call Register from their init() functions before this
// package's init runs (Go does not order init across packages).
var (
	mu       sync.RWMutex
	registry = map[string]Format{}
)

// Register adds f to the global registry under f.Name(). Intended to be
// called from a Format implementation's init() function so the binary
// picks the format up by import:
//
//	// internal/formats/pytest/pytest.go
//	func init() { formats.Register(&Format{}) }
//
// Register panics on duplicate names. This is a programmer error: two
// formats claiming the same identifier collide on the CLI and the
// detector. Catching it at init time fails the binary on startup
// rather than producing surprising behaviour at runtime. Duplicates
// only happen if a contributor has copy-pasted a format without
// renaming it.
//
// Register panics if f is nil or f.Name() returns the empty string.
// These are also programmer errors and never legitimate input.
func Register(f Format) {
	if f == nil {
		panic("formats.Register: nil Format")
	}
	name := f.Name()
	if name == "" {
		panic("formats.Register: Format.Name() returned empty string")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("formats.Register: duplicate format name %q", name))
	}
	registry[name] = f
}

// Get returns the Format registered under name and reports whether it
// exists. Lookup is case-sensitive: format names are lowercase by
// convention and the CLI does not normalise.
func Get(name string) (Format, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := registry[name]
	return f, ok
}

// All returns every registered Format, sorted alphabetically by Name.
// The returned slice is a snapshot; callers may mutate it without
// affecting the registry. Useful for "distill-ai list-formats", the
// detection fan-out, and tests.
func All() []Format {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Format, 0, len(registry))
	for _, f := range registry {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

// reset clears the registry. Intended for tests only; the production
// binary never deregisters. Unexported so external packages can't call
// it accidentally.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	registry = map[string]Format{}
}
