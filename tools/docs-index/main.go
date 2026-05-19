// Command docs-index regenerates docs/index.md from every Markdown
// file under docs/. The output is a flat link list keyed off each
// file's first `# Heading` line, grouped by directory depth.
//
// The drift-guard test TestDocsIndex_CoversEveryMarkdownFile runs
// the same scan in-process and fails when the rendered index would
// differ — so a contributor who adds a new doc must regenerate the
// index in the same commit.
//
// Usage:
//
//	go run ./tools/docs-index              # writes docs/index.md
//	go run ./tools/docs-index -o /tmp/i.md # writes elsewhere
//	go run ./tools/docs-index -check       # exit non-zero on drift
//
// Lives under tools/ so it doesn't ship with the production binary.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultDocsRoot = "docs"
const defaultOutputPath = "docs/index.md"

func main() {
	root := flag.String("root", defaultDocsRoot, "directory to scan for Markdown files")
	out := flag.String("o", defaultOutputPath, "path to write the generated index to")
	check := flag.Bool("check", false, "exit non-zero on drift; do not write")
	flag.Parse()
	rendered, err := render(*root, *out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs-index: %v\n", err)
		os.Exit(1)
	}
	if *check {
		current, err := os.ReadFile(*out) //nolint:gosec // CLI flag
		if err != nil {
			fmt.Fprintf(os.Stderr, "docs-index: read %s: %v\n", *out, err)
			os.Exit(1)
		}
		if !bytes.Equal(current, rendered) {
			fmt.Fprintf(os.Stderr, "docs-index: drift in %s — run `go run ./tools/docs-index` to regenerate\n", *out)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, rendered, 0o644); err != nil { //nolint:gosec // markdown is world-readable
		fmt.Fprintf(os.Stderr, "docs-index: write %s: %v\n", *out, err)
		os.Exit(1)
	}
}

// render scans docsRoot for *.md files (excluding the output file
// itself) and produces the index Markdown.
func render(docsRoot, outputPath string) ([]byte, error) {
	entries, err := scan(docsRoot, outputPath)
	if err != nil {
		return nil, err
	}
	return formatIndex(entries, docsRoot), nil
}

// docEntry is one row in the rendered index.
type docEntry struct {
	relPath string // path relative to repo root, e.g. "docs/envelope.md"
	title   string // the first `# ` heading, e.g. "Envelope strippers"
}

// scan walks docsRoot and returns every Markdown file with its
// first H1 heading. Files without an H1 are still listed; their
// title falls back to a humanised version of their basename so the
// index remains exhaustive even when a doc is incomplete. The
// outputPath file is excluded so its own previous rendering doesn't
// become content.
func scan(docsRoot, outputPath string) ([]docEntry, error) {
	var out []docEntry
	err := filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		// The generated index lists every other doc but not
		// itself; otherwise a regeneration would treat its own
		// previous rendering as content and the drift guard
		// would oscillate.
		if filepath.ToSlash(path) == outputPath {
			return nil
		}
		title, err := firstHeading(path)
		if err != nil {
			return err
		}
		out = append(out, docEntry{relPath: filepath.ToSlash(path), title: title})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		// Group by directory depth then alphabetically so the
		// index reads top-level docs first (envelope.md,
		// config.md), then subdirectories (formats/, decisions/).
		di := strings.Count(out[i].relPath, "/")
		dj := strings.Count(out[j].relPath, "/")
		if di != dj {
			return di < dj
		}
		return out[i].relPath < out[j].relPath
	})
	return out, nil
}

// firstHeading returns the first `# Heading` line's text, with the
// `# ` prefix stripped and whitespace trimmed. Returns a fallback
// derived from the filename when no heading is found, so every doc
// in the index has a non-empty title.
func firstHeading(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // walking a repo-local tree
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# ")), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return humanise(filepath.Base(path)), nil
}

// humanise turns a filename ("envelope.md") into a title-cased
// fallback ("envelope"). Stripping the suffix is enough; mixed-case
// names like SCHEMA.md keep their existing casing for readability.
func humanise(name string) string {
	return strings.TrimSuffix(name, ".md")
}

// formatIndex renders the entries as Markdown. The grouping is
// shallow: a header per top-level subdirectory under docsRoot (or
// the docsRoot itself), then a bullet list of links.
func formatIndex(entries []docEntry, docsRoot string) []byte {
	var sb strings.Builder
	sb.WriteString("# Documentation index\n\n")
	sb.WriteString("Generated by `go run ./tools/docs-index`. This file is\n")
	sb.WriteString("checked by `TestDocsIndex_CoversEveryMarkdownFile`; a doc\n")
	sb.WriteString("added without an index update fails CI.\n\n")
	// Group by the first segment after docs/. Top-level docs go
	// under the heading "Top-level", subdirectory docs under the
	// directory name (e.g. "Formats", "Decisions").
	groups := map[string][]docEntry{}
	for _, e := range entries {
		rest := strings.TrimPrefix(e.relPath, docsRoot+"/")
		parts := strings.SplitN(rest, "/", 2)
		group := "Top-level"
		if len(parts) > 1 {
			group = humaniseGroup(parts[0])
		}
		groups[group] = append(groups[group], e)
	}
	// Stable ordering: Top-level first, then the rest
	// alphabetically. Reads naturally for someone scanning the
	// index from a fresh visit.
	var names []string
	for name := range groups {
		if name != "Top-level" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	order := append([]string{"Top-level"}, names...)
	for _, name := range order {
		items, ok := groups[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "## %s\n\n", name)
		for _, e := range items {
			link := "./" + strings.TrimPrefix(e.relPath, docsRoot+"/")
			fmt.Fprintf(&sb, "- [%s](%s)\n", e.title, link)
		}
		sb.WriteString("\n")
	}
	return []byte(strings.TrimRight(sb.String(), "\n") + "\n")
}

// humaniseGroup turns "formats" → "Formats", "decisions" →
// "Decisions". The full title-case rule is fine for the v1 set
// (the only subdirectories are lowercase single-word names).
func humaniseGroup(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
