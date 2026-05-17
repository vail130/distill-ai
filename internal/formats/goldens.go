package formats

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
)

// updatingGoldens reports whether the current test run should
// rewrite *.expected files instead of diffing. Driven by the
// DISTILL_AI_UPDATE_GOLDENS=1 environment variable rather than a
// shared `-update` test flag, because the formats package is
// imported by the production binary; defining a top-level
// `flag.Bool` would register the flag in every binary that links
// the package (and conflict with the same flag in
// internal/output). The env-var approach keeps the harness
// importable from external test packages without polluting the
// production CLI surface.
//
// Usage:
//
//	DISTILL_AI_UPDATE_GOLDENS=1 go test ./internal/formats/generic/
//
// Contributors who prefer a flag can wrap this in a project-local
// Makefile target (`make update-goldens`).
func updatingGoldens() bool {
	return os.Getenv("DISTILL_AI_UPDATE_GOLDENS") == "1"
}

// RunGoldens walks dir for *.input fixtures, runs each through f
// using a fresh ParseOpts, marshals the emitted Events as JSON,
// and diffs against the matching *.expected file. Intended to be
// called from a format's TestFormat_Goldens test:
//
//	func TestGeneric_Goldens(t *testing.T) {
//	    formats.RunGoldens(t, generic.Format{}, "testdata")
//	}
//
// The output JSON has a stable shape: an array of Event objects in
// emission order. Determinism is the property test's job (we don't
// re-run twice here), but the harness produces byte-identical
// output for a given input so a diff signals a real change.
//
// When DISTILL_AI_UPDATE_GOLDENS=1 is set in the environment, the
// harness writes the parser's output to <name>.expected (creating
// it if missing) instead of comparing. Reviewers verify the new
// fixture by hand.
func RunGoldens(t *testing.T, f Format, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("RunGoldens: read %s: %v", dir, err)
	}
	cases := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".input") {
			cases = append(cases, strings.TrimSuffix(name, ".input"))
		}
	}
	sort.Strings(cases)
	if len(cases) == 0 {
		t.Fatalf("RunGoldens: no *.input fixtures found under %s", dir)
	}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Helper()
			inputPath := filepath.Join(dir, name+".input")
			expectedPath := filepath.Join(dir, name+".expected")
			input, err := os.ReadFile(inputPath) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("read %s: %v", inputPath, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ch, err := f.Parse(ctx, bytes.NewReader(input), ParseOpts{})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := drainEvents(ch)
			actual, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatalf("marshal events: %v", err)
			}
			actual = append(actual, '\n')
			if updatingGoldens() {
				if err := os.WriteFile(expectedPath, actual, 0o644); err != nil { //nolint:gosec // test path
					t.Fatalf("write %s: %v", expectedPath, err)
				}
				t.Logf("updated %s", expectedPath)
				return
			}
			expected, err := os.ReadFile(expectedPath) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("read %s (run with -update to create): %v", expectedPath, err)
			}
			if !bytes.Equal(actual, expected) {
				t.Errorf("%s diverged from golden\n--- expected\n%s\n--- got\n%s",
					name, expected, actual)
			}
		})
	}
}

// drainEvents reads every Event the channel emits and returns
// them in emission order. Internal helper for RunGoldens; not
// exported because individual format tests prefer their own
// inline drain for assertions on per-Event fields.
func drainEvents(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	if out == nil {
		out = []event.Event{}
	}
	return out
}

// FixtureCount is a small helper that asserts a fixture directory
// contains exactly n .input files. Used by per-format
// TestFormat_FixtureCount tests so future contributors can't
// silently delete fixtures.
func FixtureCount(t *testing.T, dir string, want int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("FixtureCount: read %s: %v", dir, err)
	}
	got := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".input") {
			got++
		}
	}
	if got != want {
		t.Fatalf("FixtureCount: %s has %d *.input fixtures; want %d", dir, got, want)
	}
}
