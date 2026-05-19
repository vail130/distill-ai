package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/formats"

	// Trigger registration of every shipped format and envelope.
	_ "github.com/vail130/distill-ai/internal/envelope/dockercompose"
	_ "github.com/vail130/distill-ai/internal/envelope/githubactions"
	_ "github.com/vail130/distill-ai/internal/envelope/gitlabci"
	_ "github.com/vail130/distill-ai/internal/formats/generic"
	_ "github.com/vail130/distill-ai/internal/formats/gotest"
	_ "github.com/vail130/distill-ai/internal/formats/gotestsum"
	_ "github.com/vail130/distill-ai/internal/formats/jest"
	_ "github.com/vail130/distill-ai/internal/formats/pytest"
)

// TestSchemaDoc_EveryKindHasProducer scans docs/formats/SCHEMA.md
// for the per-format kind lists (the bullets under "### Kind
// values") and asserts each format named has a registered Format
// and each kind named appears in that format's emitted-kinds set.
//
// The drift catch: a kind added to SCHEMA.md without a matching
// parser change (or vice versa) fails CI. Pairs with the per-format
// TestXxx_DocumentedKindsMatchEmitted below, which checks the
// reverse direction.
func TestSchemaDoc_EveryKindHasProducer(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "docs", "formats", "SCHEMA.md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read SCHEMA.md: %v", err)
	}
	body := string(raw)
	// The "Kind values" section names each format on its own bullet
	// of the form `**name**: kind1, kind2, kind3`. The block ends at
	// the next "###" heading. Find the block, parse each bullet.
	startMarker := "### Kind values"
	endMarker := "###"
	si := strings.Index(body, startMarker)
	if si < 0 {
		t.Fatalf("SCHEMA.md missing %q section", startMarker)
	}
	rest := body[si+len(startMarker):]
	ei := strings.Index(rest, endMarker)
	if ei < 0 {
		t.Fatalf("SCHEMA.md %q section has no terminator", startMarker)
	}
	block := rest[:ei]
	// Match `**fmt**: kind1, kind2, ...`
	bulletPat := regexp.MustCompile(`(?m)^\*\*([a-zA-Z0-9_-]+)\*\*:\s*([^\n]+)$`)
	for _, m := range bulletPat.FindAllStringSubmatch(block, -1) {
		fmtName := m[1]
		// Skip the custom open-set entry — it is documented as
		// "open-set" and intentionally not enumerated.
		if strings.Contains(fmtName, "custom") {
			continue
		}
		got, ok := formats.Get(fmtName)
		if !ok {
			t.Errorf("SCHEMA.md names format %q in Kind values but no such format is registered", fmtName)
			continue
		}
		docKinds := splitKindList(m[2])
		emitted := emittedKinds(t, fmtName)
		for _, k := range docKinds {
			if _, ok := emitted[k]; !ok {
				t.Errorf("SCHEMA.md says format %q emits kind %q but the parser source does not contain that string literal",
					got.Name(), k)
			}
		}
	}
}

// TestPerFormatDocs_KindsMatch is the reverse of
// TestSchemaDoc_EveryKindHasProducer: it parses each format's
// `## Event kinds emitted` table and asserts the documented kinds
// are exactly the kinds the parser source contains.
//
// Each format's doc was extended in M16.3 with a quick-reference
// table whose first column is the kind literal. The table is
// recognised by a leading "| `kind`" header row.
func TestPerFormatDocs_KindsMatch(t *testing.T) {
	for _, name := range []string{"generic", "gotest", "gotestsum", "pytest", "jest"} {
		name := name
		t.Run(name, func(t *testing.T) {
			documented := docKindsFor(t, name)
			emitted := emittedKinds(t, name)
			for _, k := range documented {
				if _, ok := emitted[k]; !ok {
					t.Errorf("%s.md documents kind %q but the parser source does not emit it",
						name, k)
				}
			}
			// Reverse direction: the parser emits a kind the
			// doc never names. The emitted-kind scan is a
			// best-effort string-literal scan; some kinds are
			// computed at runtime, so the reverse check is
			// informational rather than hard. Log unexpected
			// emissions for visibility but do not fail.
			for k := range emitted {
				if !contains(documented, k) {
					t.Logf("%s parser emits kind %q which is not documented in %s.md (may be intentional if the value is computed)",
						name, k, name)
				}
			}
		})
	}
}

// TestEnvelopeDoc_EveryStripperDocumented asserts that every
// registered envelope.Stripper has a corresponding `### <name>`
// subsection in docs/envelope.md. New strippers must update the
// doc in the same commit; this is the drift guard.
func TestEnvelopeDoc_EveryStripperDocumented(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "docs", "envelope.md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read envelope.md: %v", err)
	}
	body := string(raw)
	for _, s := range envelope.All() {
		if s.Name() == envelope.ChoiceNone {
			continue // noop stripper has its own dedicated section title
		}
		// A registered stripper must appear as a `### name`
		// subsection somewhere in the doc.
		needle := "### " + s.Name()
		if !strings.Contains(body, needle) {
			t.Errorf("envelope.md missing %q subsection for registered stripper %q",
				needle, s.Name())
		}
	}
}

// TestADRIndex_ListsEveryADR cross-checks the table in
// docs/decisions/README.md against the files actually present in
// the directory. A new ADR added without an index update fails CI.
func TestADRIndex_ListsEveryADR(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	dir := filepath.Join(root, "docs", "decisions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var adrs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "README.md" {
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		adrs = append(adrs, name)
	}
	sort.Strings(adrs)
	indexRaw, err := os.ReadFile(filepath.Join(dir, "README.md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	indexBody := string(indexRaw)
	for _, name := range adrs {
		if !strings.Contains(indexBody, name) {
			t.Errorf("ADR index (docs/decisions/README.md) missing %q; add a table row",
				name)
		}
	}
	// Reverse: the index must not link to ADR filenames that
	// don't exist (a rename without an index update).
	linkPat := regexp.MustCompile(`\]\(\./(\d{4}-[a-z0-9-]+\.md)\)`)
	for _, m := range linkPat.FindAllStringSubmatch(indexBody, -1) {
		linked := m[1]
		full := filepath.Join(dir, linked)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("ADR index links to %q but no such file exists: %v",
				linked, err)
		}
	}
}

// TestDocsIndex_CoversEveryMarkdownFile runs the docs-index
// generator in -check mode and asserts the on-disk
// docs/index.md matches the rendered output. A new doc added
// without regenerating the index fails CI.
//
// The generator binary itself ships its own determinism — the
// only check here is "is the committed index up-to-date?".
func TestDocsIndex_CoversEveryMarkdownFile(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	cmd := exec.Command("go", "run", "./tools/docs-index", //nolint:gosec // tooling path is repo-local
		"-root", "docs", "-o", "docs/index.md", "-check")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("docs-index -check failed: %v\nstderr=%s", err, stderr.String())
	}
}

// -----------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------

// splitKindList parses a comma-separated kind list like
// "`test_failure`, `panic`, `build_failure`" into ["test_failure",
// "panic", "build_failure"].
func splitKindList(s string) []string {
	pat := regexp.MustCompile("`([a-zA-Z0-9_]+)`")
	matches := pat.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// docKindsFor reads docs/formats/<name>.md and returns the kinds
// listed in the leading "Event kinds emitted" table. The first
// column is the kind literal in backticks.
func docKindsFor(t *testing.T, name string) []string {
	t.Helper()
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "docs", "formats", name+".md")) //nolint:gosec // repo-local
	if err != nil {
		t.Fatalf("read %s.md: %v", name, err)
	}
	body := string(raw)
	startMarker := "## Event kinds emitted"
	si := strings.Index(body, startMarker)
	if si < 0 {
		t.Fatalf("%s.md missing %q section", name, startMarker)
	}
	rest := body[si+len(startMarker):]
	endMarker := "\n## "
	ei := strings.Index(rest, endMarker)
	if ei < 0 {
		ei = len(rest)
	}
	block := rest[:ei]
	// Rows are `| `kind` | sev | desc |` — extract the first
	// backticked column. The Markdown header itself is `| \`kind\`
	// |`, which matches; skip the literal string "kind" since it
	// only appears in the header row.
	rowPat := regexp.MustCompile("(?m)^\\|\\s*`([a-zA-Z0-9_]+)`")
	var out []string
	for _, m := range rowPat.FindAllStringSubmatch(block, -1) {
		if m[1] == "kind" {
			continue // header row marker, not an actual kind
		}
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatalf("%s.md Event kinds table has no rows", name)
	}
	return out
}

// emittedKinds returns the set of string-literal Kind values that
// appear in the format's source tree. The scan is shallow — it
// looks for the documented kind strings in any .go file under
// internal/formats/<name>/ — and is good enough for the drift
// guard's purpose (catch a kind named in docs that nobody emits).
func emittedKinds(t *testing.T, name string) map[string]struct{} {
	t.Helper()
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	dir := filepath.Join(root, "internal", "formats", name)
	out := map[string]struct{}{}
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path) //nolint:gosec // repo-local
		if err != nil {
			return err
		}
		// The known kind catalogue across all v1 formats; any
		// string-literal occurrence of these counts as the
		// parser emitting that kind. Open-set future formats
		// (custom) are not enumerated here.
		for _, k := range allKnownKinds {
			if bytes.Contains(raw, []byte("\""+k+"\"")) {
				out[k] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

var allKnownKinds = []string{
	// generic
	"error_line",
	"warning_line",
	"traceback",
	"exception",
	// gotest
	"test_failure",
	"panic",
	"build_failure",
	"race_condition",
	// gotestsum reuses gotest failure kinds.
	// pytest
	"test_error",
	"collection_error",
	"warning",
	// jest
	"snapshot_mismatch",
	"suite_error",
}

func contains(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}
