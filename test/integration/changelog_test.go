package integration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// keepAChangelogSubsections is the canonical set per the
// Keep a Changelog 1.1.0 grammar. The drift guard rejects any
// `### Heading` that isn't one of these.
var keepAChangelogSubsections = map[string]bool{
	"Added":      true,
	"Changed":    true,
	"Deprecated": true,
	"Removed":    true,
	"Fixed":      true,
	"Security":   true,
}

// TestChangelog_HasUnreleasedAndV1Section pins the M16.5 layout:
// the file opens with an `[Unreleased]` block (pre-stubbed for
// future entries) followed by a `[1.0.0] - DATE` block. A
// contributor who forgets to add the new-version section under
// the next release fails here.
func TestChangelog_HasUnreleasedAndV1Section(t *testing.T) {
	body := readChangelog(t)
	if !strings.Contains(body, "## [Unreleased]") {
		t.Errorf("CHANGELOG.md missing the `## [Unreleased]` section header")
	}
	// The 1.0.0 section uses a date placeholder until M17.5 fills it.
	// Accept either the placeholder "YYYY-MM-DD" or an ISO date so
	// the test stays green after the release tag.
	v1Pat := regexp.MustCompile(`## \[1\.0\.0\] - (YYYY-MM-DD|\d{4}-\d{2}-\d{2})`)
	if !v1Pat.MatchString(body) {
		t.Errorf("CHANGELOG.md missing the `## [1.0.0] - DATE` section header (or its placeholder)")
	}
}

// TestChangelog_SubsectionHeadersAreSemVer enforces the
// Keep a Changelog 1.1.0 subsection grammar: every `### Heading`
// must be one of Added / Changed / Deprecated / Removed / Fixed /
// Security. A new `### Refactored` or `### Tests` fails here so
// future contributors stay inside the published grammar.
//
// The check also rejects duplicate subsection headers under the
// same version: a section that opens `### Added` twice means an
// earlier merge was sloppy.
func TestChangelog_SubsectionHeadersAreSemVer(t *testing.T) {
	body := readChangelog(t)
	subsections := map[string]int{}
	currentVersion := ""
	scanner := strings.Split(body, "\n")
	for _, line := range scanner {
		// Track the version we're under so dup detection is
		// scoped per-version.
		if strings.HasPrefix(line, "## [") {
			// Each new version resets the per-section counter.
			currentVersion = line
			subsections = map[string]int{}
			continue
		}
		if !strings.HasPrefix(line, "### ") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "### "))
		if !keepAChangelogSubsections[name] {
			t.Errorf("CHANGELOG.md uses non-Keep-a-Changelog subsection %q under %q; allowed: Added/Changed/Deprecated/Removed/Fixed/Security",
				name, currentVersion)
			continue
		}
		subsections[name]++
		if subsections[name] > 1 {
			t.Errorf("CHANGELOG.md has duplicate %q subsection under %q; consolidate entries into a single block",
				name, currentVersion)
		}
	}
}

// TestChangelog_EveryClosedMilestoneHasEntry walks TODO.md for
// every "## M<N> ... ✅" heading and asserts CHANGELOG.md
// mentions the milestone identifier. The check is loose — a
// substring match against "M9" / "M10" etc. is enough to catch
// the case where a closed milestone landed without a CHANGELOG
// entry.
//
// Internal-contract milestones (M0-M7) shipped before the
// CHANGELOG existed and are explicitly skipped — they correspond
// to the foundational types, pipeline plumbing, and infrastructure
// that the user-visible milestones (M8 onward) build on. Adding
// retroactive entries for them would amount to docs-archaeology.
func TestChangelog_EveryClosedMilestoneHasEntry(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	todoRaw, err := os.ReadFile(filepath.Join(root, "TODO.md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read TODO.md: %v", err)
	}
	body := readChangelog(t)
	// Top-level milestone heading shape:
	//   ## M9 — Generic format (fallback) ✅
	// The trailing checkmark indicates closed.
	pat := regexp.MustCompile(`(?m)^## (M\d+)\s+—.*✅\s*$`)
	matches := pat.FindAllStringSubmatch(string(todoRaw), -1)
	closed := make([]string, 0, len(matches))
	for _, m := range matches {
		mile := m[1]
		// Skip internal-contract milestones that pre-dated
		// the CHANGELOG. The cutoff is the first user-visible
		// CLI surface (M8).
		switch mile {
		case "M0", "M1", "M2", "M3", "M4", "M5", "M6", "M7":
			continue
		}
		closed = append(closed, mile)
	}
	sort.Strings(closed)
	for _, mile := range closed {
		// Word-boundary match so "M1" doesn't satisfy "M14".
		needle := regexp.MustCompile(`\b` + mile + `(?:[.\s:_]|$)`)
		if !needle.MatchString(body) {
			t.Errorf("CHANGELOG.md has no entry for closed milestone %q; add a line under the relevant version section",
				mile)
		}
	}
}

// readChangelog returns CHANGELOG.md's body, t.Fatal on error.
func readChangelog(t *testing.T) string {
	t.Helper()
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	return string(raw)
}
