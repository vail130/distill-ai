package integration_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublicAPI_NoLeakedInternalImports asserts the pkg/distill
// import contract: the publicly-importable surface (everything
// directly under pkg/distill/) may only depend on stdlib,
// internal/event (the type-alias target from M1.4), or
// internal/formats (also a type-alias target). Every other
// internal/* dependency must go through the
// pkg/distill/internal/orchestrator subpackage.
//
// This protects the layering described in
// ARCHITECTURE.md § Library API: pkg/distill is for libraries;
// internal/ is for distill-ai; orchestrator is the bridge. An
// accidental import of, say, internal/pipeline directly from
// pkg/distill/distill.go would leak implementation detail into
// the public surface and prevent future refactoring without a
// version bump.
//
// The test does NOT check pkg/distill/internal/* — those packages
// are themselves internal to pkg/distill and may import any
// internal/* package they like.
func TestPublicAPI_NoLeakedInternalImports(t *testing.T) {
	allowed := map[string]bool{
		"github.com/vail130/distill-ai/internal/event":   true,
		"github.com/vail130/distill-ai/internal/formats": true,
		// Side-effect imports in register.go bring formats and
		// envelope strippers into the global registry. They're
		// not part of the public type surface — they only
		// trigger init() — so the test allows them despite
		// being internal/ packages.
		"github.com/vail130/distill-ai/internal/envelope/githubactions": true,
		"github.com/vail130/distill-ai/internal/envelope/gitlabci":      true,
		"github.com/vail130/distill-ai/internal/formats/generic":        true,
		"github.com/vail130/distill-ai/internal/formats/gotest":         true,
		"github.com/vail130/distill-ai/internal/formats/jest":           true,
		"github.com/vail130/distill-ai/internal/formats/pytest":         true,
	}
	root := repoRoot(t)
	pkgDir := filepath.Join(root, "pkg", "distill")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read pkg/distill: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(pkgDir, name)
		af, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range af.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(ip, "github.com/vail130/distill-ai/internal/") {
				continue
			}
			if !allowed[ip] {
				t.Errorf("%s imports disallowed internal package %q; "+
					"route the dependency through "+
					"pkg/distill/internal/orchestrator instead",
					name, ip)
			}
		}
	}
}

// TestPublicAPI_OrchestratorIsPrivate is a structural guard: the
// orchestrator subpackage lives under pkg/distill/internal/, so
// Go's standard `internal/` visibility rule already forbids
// imports from outside the pkg/distill subtree. The test
// double-checks by inspecting the directory layout — if a future
// refactor moved orchestrator to a non-internal path, this test
// would catch it before review.
func TestPublicAPI_OrchestratorIsPrivate(t *testing.T) {
	root := repoRoot(t)
	want := filepath.Join(root, "pkg", "distill", "internal", "orchestrator")
	st, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat %s: %v", want, err)
	}
	if !st.IsDir() {
		t.Fatalf("%s is not a directory", want)
	}
	// Walk the path components and assert "internal" appears
	// between pkg/distill and the package leaf.
	rel, err := filepath.Rel(filepath.Join(root, "pkg", "distill"), want)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 || parts[0] != "internal" {
		t.Errorf("orchestrator path %q does not include /internal/; "+
			"Go's visibility rule won't protect it", rel)
	}
}

// repoRoot walks upward from the test's working directory to find
// the directory containing go.mod. The integration package can
// move; this keeps the public-API test robust against future
// reorganisation.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		wd = filepath.Dir(wd)
	}
	t.Fatalf("repoRoot: go.mod not found")
	return ""
}
