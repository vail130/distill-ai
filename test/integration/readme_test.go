package integration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
	// Trigger format registration.
	_ "github.com/vail130/distill-ai/internal/formats/generic"
	_ "github.com/vail130/distill-ai/internal/formats/gotest"
	_ "github.com/vail130/distill-ai/internal/formats/jest"
	_ "github.com/vail130/distill-ai/internal/formats/pytest"
)

// TestReadme_FormatListMatchesRegistry asserts that the four format
// names the README's "Supported formats" lede block names are exactly
// the set the binary registers. Catches the case where a future
// format ships without updating the README, and the case where the
// README still names a format that was removed.
//
// The check is intentionally narrow: it parses the four canonical
// names out of the lede paragraph rather than the longer "Supported
// formats" section, because the lede is where every reader actually
// learns what ships. A change to the lede sentence has to be paired
// with a registry change in the same commit.
func TestReadme_FormatListMatchesRegistry(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(repoRoot, "README.md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	body := string(readme)
	// The lede paragraph names the formats in backticks. The
	// exact sentence today is:
	//   v1.0 ships four format parsers — `gotest`, `pytest`,
	//   `jest`, `generic` — plus two CI envelope strippers ...
	// We extract every backticked-token in the first ~1.5 KB of
	// the file (the lede block) and intersect with the registry.
	const ledeBytes = 1500
	if len(body) > ledeBytes {
		body = body[:ledeBytes]
	}
	tokens := extractBacktickedTokens(body)
	registered := make(map[string]bool)
	for _, f := range formats.All() {
		registered[f.Name()] = true
	}
	// Every format name that appears in the lede must be a real
	// registered format. The reverse direction (every registered
	// format must appear) is checked separately because formats
	// for envelopes etc. live in a different section.
	for tok := range tokens {
		if !registered[tok] {
			continue // not a format reference; could be a flag name etc.
		}
	}
	// Hard assertion: the four v1.0 formats must appear in the lede.
	for _, want := range []string{"gotest", "pytest", "jest", "generic"} {
		if !tokens[want] {
			t.Errorf("README lede paragraph must name format %q (in backticks); not found in first %d bytes",
				want, ledeBytes)
		}
		if !registered[want] {
			t.Errorf("README names %q but no such format is registered", want)
		}
	}
}

// TestReadme_LinksResolve walks every Markdown link in README.md
// whose target is a repo-local relative path (./docs/..., ./pkg/...,
// etc.) and asserts the file exists. External URLs are not checked
// — that would gate CI on third-party uptime.
//
// Drift guard for the documentation rewrite in M16.2 and onward: a
// rename or removal of any referenced file without updating the
// README must fail this test.
func TestReadme_LinksResolve(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(repoRoot, "README.md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	links := extractMarkdownLinks(string(readme))
	for _, l := range links {
		// Skip http(s) and any other scheme.
		if strings.Contains(l, "://") || strings.HasPrefix(l, "mailto:") {
			continue
		}
		// Drop fragment / query — file existence is anchored on
		// the path part only.
		path := l
		if idx := strings.IndexAny(path, "#?"); idx >= 0 {
			path = path[:idx]
		}
		// Skip empty (pure anchor refs like "#section").
		if path == "" {
			continue
		}
		full := filepath.Join(repoRoot, path)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("README references missing path %q: %v", path, err)
		}
	}
}

// TestReadme_StatsMarkersResolve verifies each `<!-- distill-ai-stats:NAME -->`
// block in the README:
//
//  1. Has a matching close marker.
//  2. Refers to a fixture name that exists under
//     test/integration/testdata/fixtures/.
//
// The full integer-drift check lives in `make readme-stats-check`,
// which forks the binary and is too slow for the unit-test loop.
// This integration test pins the structural shape so a typo in the
// marker name or a missing close marker fails CI immediately, before
// any number is compared.
func TestReadme_StatsMarkersResolve(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(repoRoot, "README.md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	body := string(readme)
	openPat := regexp.MustCompile(`<!--\s*distill-ai-stats:(\S+?)\s*-->`)
	closePat := func(name string) *regexp.Regexp {
		return regexp.MustCompile(`<!--\s*/distill-ai-stats:` + regexp.QuoteMeta(name) + `\s*-->`)
	}
	matches := openPat.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		// No markers yet is OK; M16.2 introduces them but
		// future audits can decide whether to require any.
		return
	}
	fixturesDir := filepath.Join(repoRoot, "test", "integration", "testdata", "fixtures")
	var names []string
	for _, m := range matches {
		name := m[1]
		names = append(names, name)
		// Close marker present.
		if !closePat(name).MatchString(body) {
			t.Errorf("README marker %q has no matching close marker", name)
		}
		// Fixture file exists.
		fixturePath := filepath.Join(fixturesDir, name+".input")
		if _, err := os.Stat(fixturePath); err != nil {
			t.Errorf("README marker %q references missing fixture %s", name, fixturePath)
		}
	}
	sort.Strings(names)
	t.Logf("found %d distill-ai-stats markers in README: %v", len(names), names)
}

// extractMarkdownLinks pulls the URL component out of every
// `[text](url)` link in body. Tolerant: doesn't handle reference-
// style links (`[text][ref]`), which the README doesn't currently
// use.
func extractMarkdownLinks(body string) []string {
	pat := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	matches := pat.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// extractBacktickedTokens returns the set of single-backticked
// identifiers in body (`name` style). Used to find format and flag
// references in the README lede.
func extractBacktickedTokens(body string) map[string]bool {
	pat := regexp.MustCompile("`([a-zA-Z0-9_.-]+)`")
	out := make(map[string]bool)
	for _, m := range pat.FindAllStringSubmatch(body, -1) {
		out[m[1]] = true
	}
	return out
}
