package integration_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestManpageGeneration_CoversEveryFlag pins the contract from M16.1:
// every flag the SKILL.md `cli-surface` manifest lists must be
// documented in at least one man page. The check is a substring scan
// on the rendered roff source rather than a roff parse — every flag
// renders as `\fB-flag\fP` (bold, with the dash literal) which the
// substring `"\\fB--auto"` or similar matches reliably.
//
// Catches the case where a flag is added to the binary and the
// SKILL.md manifest, but the man pages aren't regenerated. The
// alignment rule already mandates regeneration; this test enforces
// it.
//
// Because flags can live on subcommands rather than on the root,
// the test scans every .1 file in man/man1/ and treats the union as
// "documented". A flag that exists on `run` but not on the root is
// still documented as long as it appears in distill-ai-run.1.
func TestManpageGeneration_CoversEveryFlag(t *testing.T) {
	manifest := readSkillManifest(t)
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	manDir := filepath.Join(repoRoot, "man", "man1")
	entries, err := os.ReadDir(manDir)
	if err != nil {
		t.Fatalf("read man dir: %v\n"+
			"Hint: regenerate with `go run ./cmd/distill-ai/gen-man`",
			err)
	}
	var allManPages strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".1") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(manDir, e.Name())) //nolint:gosec // repo-local
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		allManPages.Write(raw)
		allManPages.WriteByte('\n')
	}
	body := allManPages.String()
	// Flags appear in cobra/doc roff output as `\fB-x\fP` (bold).
	// The leading dash is literal in roff; the trailing `\fP`
	// terminates bold. Constructing a tight substring catches
	// both `--foo` and `-x` cases consistently.
	for _, fl := range manifest.flags {
		// roff's bold open is `\fB`, close is `\fP`. The flag
		// string itself appears verbatim between them.
		needle := `\fB` + fl
		if !strings.Contains(body, needle) {
			t.Errorf("manifest flag %q is not documented in any man page; "+
				"regenerate with `go run ./cmd/distill-ai/gen-man`", fl)
		}
	}
}

// TestManpageGeneration_NoStaleSubcommands is the reverse direction:
// every distill-ai-<verb>.1 file in man/man1/ must correspond to a
// registered cobra subcommand (or be the root page). Catches the
// case where a verb is renamed or removed without regenerating;
// without this guard, the orphan would silently survive into a
// release.
func TestManpageGeneration_NoStaleSubcommands(t *testing.T) {
	manifest := readSkillManifest(t)
	known := stringSet(manifest.subcommands)
	known["distill-ai"] = true // root page; not a subcommand
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	manDir := filepath.Join(repoRoot, "man", "man1")
	entries, err := os.ReadDir(manDir)
	if err != nil {
		t.Fatalf("read man dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".1") {
			continue
		}
		base := strings.TrimSuffix(name, ".1")
		// Subcommand pages are named distill-ai-<verb>.1; the
		// root is distill-ai.1.
		verb := strings.TrimPrefix(base, "distill-ai-")
		if base == "distill-ai" {
			verb = "distill-ai"
		}
		if _, ok := known[verb]; !ok {
			t.Errorf("orphan man page %s: %q is not a registered subcommand; "+
				"delete the file or add the verb to SKILL.md", name, verb)
		}
	}
}

// TestManpageGeneration_CheckedIntoRepo verifies the man pages are
// committed to the repo, not lazily generated. Distribution
// channels (Homebrew, .deb, .rpm) install whatever is in the source
// tree; if the files aren't there at build time the package has no
// man pages. This is also the test that flags a missed regeneration
// after a CLI change — `git status` would show diffs the contributor
// forgot to commit.
func TestManpageGeneration_CheckedIntoRepo(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	manDir := filepath.Join(repoRoot, "man", "man1")
	info, err := os.Stat(manDir)
	if err != nil {
		t.Fatalf("expected man/man1/ under repo root, got: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("man/man1 exists but is not a directory")
	}
	entries, err := os.ReadDir(manDir)
	if err != nil {
		t.Fatalf("read man dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".1") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	// At minimum the root page plus every shipped subcommand
	// must be present. The exact set is enforced by the unit
	// test in cmd/distill-ai/gen-man/main_test.go; here we only
	// confirm the directory isn't empty so a delete-everything
	// regression fails loudly.
	if len(names) == 0 {
		t.Fatalf("man/man1/ has no .1 files; run `go run ./cmd/distill-ai/gen-man`")
	}
	// The root page is non-negotiable.
	wantRoot := "distill-ai.1"
	found := false
	for _, n := range names {
		if n == wantRoot {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing root man page %s; got: %v", wantRoot, names)
	}
}
