package integration_test

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
	// Trigger format registration for the registry lookups below.
	_ "github.com/vail130/distill-ai/internal/formats/generic"
	_ "github.com/vail130/distill-ai/internal/formats/gotest"
	_ "github.com/vail130/distill-ai/internal/formats/gotestsum"
	_ "github.com/vail130/distill-ai/internal/formats/jest"
	_ "github.com/vail130/distill-ai/internal/formats/pytest"
)

// integrationDocPaths is the canonical list of M16.4 integration
// recipes the drift guards crawl. Adding a new recipe requires
// extending the list — the test failure points the contributor at
// the right edit.
var integrationDocPaths = []string{
	"docs/integration-claude-code.md",
	"docs/integration-opencode.md",
	"docs/integration-ci.md",
}

// TestIntegrationDocs_ExamplesParse asserts that every fenced
// `bash` and `yaml` code block in the integration recipes is
// syntactically parseable. It is a lightweight sanity guard, not a
// full executable check: bash blocks are validated by `bash -n`
// (parse-only mode); yaml blocks by yaml.Unmarshal via a tiny
// in-process parse, except — since we don't depend on a YAML
// library outside dev tooling — yaml is validated structurally by
// asserting the block isn't empty and every line is either blank,
// a comment, a key/value, or a known continuation shape.
//
// The bash check uses the system `bash` binary's `-n` flag, which
// performs syntactic parsing without execution. The check is
// skipped on platforms without bash on PATH (Windows CI runners
// without WSL); the integration suite is already linux/darwin-only
// in practice.
func TestIntegrationDocs_ExamplesParse(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	for _, p := range integrationDocPaths {
		p := p
		t.Run(p, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, p)) //nolint:gosec // repo-local
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			blocks := extractFencedBlocks(string(raw))
			if len(blocks) == 0 {
				t.Fatalf("%s contains no fenced code blocks; the recipe is meant to be example-heavy", p)
			}
			seenBash := false
			seenYAML := false
			for _, b := range blocks {
				switch b.lang {
				case "bash", "sh":
					seenBash = true
					checkBashParses(t, p, b.body)
				case "yaml", "yml":
					seenYAML = true
					checkYAMLShape(t, p, b.body)
				}
			}
			// Each integration doc has a use for at least one
			// shell example. The CI doc additionally has YAML.
			if !seenBash {
				t.Errorf("%s has no bash/sh code blocks; the recipe should show shell invocations", p)
			}
			if strings.Contains(p, "integration-ci") && !seenYAML {
				t.Errorf("%s is the CI doc but has no yaml code blocks; the recipe should show pipeline config", p)
			}
		})
	}
}

// TestIntegrationDocs_ReferenceShippedFormats asserts that every
// format name mentioned in a backticked-literal context in the
// integration recipes is a real registered Format. A doc that
// references a fictional or renamed format fails CI.
//
// The check is intentionally narrow: it parses backticked tokens
// inside the integration docs and intersects with a known-formats
// set. Prose mentions ("pytest" without backticks) are not
// checked because they're often used in colloquial English
// ("pytest output") rather than as a literal format identifier.
func TestIntegrationDocs_ReferenceShippedFormats(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	registered := map[string]bool{}
	for _, f := range formats.All() {
		registered[f.Name()] = true
	}
	// The set of tokens we treat as a format reference. Any
	// backticked token that matches one of these must resolve to
	// a registered format. Other backticked tokens (flag names,
	// commands, file paths) are ignored.
	candidateFormats := []string{"pytest", "gotest", "gotestsum", "jest", "generic"}
	for _, p := range integrationDocPaths {
		p := p
		t.Run(p, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, p)) //nolint:gosec // repo-local
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			tokens := extractBacktickedTokens(string(raw))
			for _, name := range candidateFormats {
				if tokens[name] && !registered[name] {
					t.Errorf("%s references format %q in backticks but no such format is registered", p, name)
				}
			}
		})
	}
}

// TestIntegrationDocs_ReferenceShippedFlags asserts that every
// long-form `--flag` reference in the integration recipes appears
// in the SKILL.md cli-surface manifest (or its future block). A
// doc that references a fictional or renamed flag fails CI.
//
// The reverse direction — flags in the manifest that no doc
// references — is NOT enforced. Not every flag deserves a recipe
// callout.
func TestIntegrationDocs_ReferenceShippedFlags(t *testing.T) {
	manifest := readSkillManifest(t)
	known := stringSet(manifest.flags)
	// The future block lists not-yet-wired flags; tolerate them
	// in docs even if the live binary doesn't accept them.
	for _, fl := range manifest.future {
		known[fl] = true
	}
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	flagPat := regexp.MustCompile(`--[a-z][a-z0-9-]*`)
	for _, p := range integrationDocPaths {
		p := p
		t.Run(p, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, p)) //nolint:gosec // repo-local
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			for _, m := range flagPat.FindAllString(string(raw), -1) {
				// distill-ai-specific flag references only.
				// Allow common CI / shell flags that aren't
				// part of distill-ai's surface (e.g.
				// --job=, --log).
				if !relevantFlag(m) {
					continue
				}
				if !known[m] {
					t.Errorf("%s references flag %q which is not in SKILL.md cli-surface manifest", p, m)
				}
			}
		})
	}
}

// extractFencedBlocks pulls every fenced code block out of body
// and returns (lang, content) for each. The fence opener is
// either ``` or ```lang.
type fencedBlock struct {
	lang string
	body string
}

func extractFencedBlocks(body string) []fencedBlock {
	var out []fencedBlock
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		if !strings.HasPrefix(line, "```") {
			i++
			continue
		}
		lang := strings.TrimPrefix(line, "```")
		lang = strings.TrimSpace(lang)
		i++
		start := i
		for i < len(lines) && !strings.HasPrefix(lines[i], "```") {
			i++
		}
		if i >= len(lines) {
			// Unterminated fence; tolerate and stop.
			break
		}
		out = append(out, fencedBlock{
			lang: lang,
			body: strings.Join(lines[start:i], "\n"),
		})
		i++
	}
	return out
}

// checkBashParses runs `bash -n` against body. Failures fail the
// test with the bash error. Skipped when no bash on PATH.
func checkBashParses(t *testing.T, doc, body string) {
	t.Helper()
	// Allow shell prompts ("$ ", "> ") in worked-example blocks
	// — strip them before validation so a `$ pytest tests/` line
	// passes bash -n. Without the strip every example block fails.
	body = stripShellPrompts(body)
	bashPath, err := osexec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not on PATH; skipping syntax check for %s", doc)
	}
	cmd := osexec.Command(bashPath, "-n") //nolint:gosec // bashPath is from LookPath
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// One forgivable case: the example contains an output
		// transcript (a runner's stdout) rather than a script.
		// Heuristic: if every non-blank line starts with a
		// timestamp, a `[N]` event header, or contains `→`
		// (the distill-ai summary arrow), treat the block as
		// transcript and skip.
		if isTranscript(body) {
			return
		}
		t.Errorf("bash -n failed on %s code block: %v\n--- block ---\n%s\n--- stderr ---\n%s",
			doc, err, body, string(out))
	}
}

// stripShellPrompts removes leading "$ " / "> " characters that
// worked-example blocks use to denote prompts but `bash -n`
// rejects as syntax errors.
func stripShellPrompts(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "$ "):
			out[i] = strings.TrimPrefix(line, "$ ")
		case line == "$":
			out[i] = ""
		default:
			out[i] = line
		}
	}
	return strings.Join(out, "\n")
}

// isTranscript heuristic for "this block is a captured run, not a
// script to validate". Used to skip `bash -n` on output blocks
// that happen to be tagged with ```bash for syntax-highlighting
// purposes.
func isTranscript(body string) bool {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) == 0 {
		return false
	}
	transcriptHits := 0
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "events from "):
			transcriptHits++
		case strings.HasPrefix(line, "["):
			// `[1] ERROR ...` event header
			transcriptHits++
		case strings.Contains(line, "→"):
			// "distilled N lines → M lines" footer
			transcriptHits++
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "==="):
			// pytest banner / distill-ai separator
			transcriptHits++
		}
	}
	// Threshold of 2 catches transcripts of any meaningful size
	// without false-positiving on real scripts that incidentally
	// echo separators.
	return transcriptHits >= 2
}

// checkYAMLShape is a structural sanity check: the block has at
// least one key:value line, and every non-blank line either is
// a comment, a key:value, a list bullet, or a continuation.
// Avoids pulling a YAML library into the integration suite.
func checkYAMLShape(t *testing.T, doc, body string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) == 0 {
		t.Errorf("%s contains an empty yaml block", doc)
		return
	}
	keyPat := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]*:`)
	bulletPat := regexp.MustCompile(`^\s*-\s`)
	sawKey := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
		case strings.HasPrefix(trimmed, "#"):
		case keyPat.MatchString(trimmed):
			sawKey = true
		case bulletPat.MatchString(line):
		case strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"):
			// continuation / nested content
		default:
			t.Errorf("%s yaml block has malformed line %q", doc, line)
			return
		}
	}
	if !sawKey {
		t.Errorf("%s yaml block has no key:value lines", doc)
	}
}

// relevantFlag filters out flags that aren't part of distill-ai's
// surface. These appear in the integration docs because the
// recipes also invoke gh, glab, kubectl, etc.
func relevantFlag(s string) bool {
	// Allow-list of CI / shell tools whose flags can appear in
	// the recipes without being part of distill-ai's manifest.
	// If a flag matches one of these prefixes when invoked in
	// context (a `gh run view --log` line, say), it's not
	// distill-ai's problem.
	nonDistillFlags := map[string]bool{
		"--log":                 true, // gh run view --log
		"--job":                 true, // glab ci trace --job=
		"--body-file":           true, // gh pr comment --body-file
		"--upload-artifact":     true,
		"--from-literal":        true,
		"--noStackTrace":        true,
		"--coverage":            true, // jest
		"--ci":                  true, // jest
		"--reporters":           true, // jest
		"--inline":              true,
		"--message-format":      true, // cargo (in KNOWN_ISSUES)
		"--tb":                  true, // pytest's traceback flag (not a distill-ai flag)
		"--message-format=json": true,
		"--out-format":          true,
	}
	if nonDistillFlags[s] {
		return false
	}
	// Common shell/CI flag names that appear in recipes.
	switch s {
	case "--watch", "--logs", "--container", "--namespace", "--follow", "--quiet":
		return false
	}
	return true
}
