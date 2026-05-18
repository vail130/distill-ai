// Command gen-man generates man pages for distill-ai from the cobra
// command tree.
//
// The pages live in man/man1/ at the repo root and are checked into
// git so distributions (Homebrew formula, .deb / .rpm bundles) can
// pick them up without re-running the generator on the install host.
//
// Usage:
//
//	go run ./cmd/distill-ai/gen-man              # writes to ./man/man1/
//	go run ./cmd/distill-ai/gen-man -o /tmp/man  # writes to /tmp/man/man1/
//
// The Makefile's `man` target wraps this. Regenerating produces
// byte-identical output on the same source SHA because the date /
// version metadata cobra/doc would normally inject is stripped — see
// the comment on stripVolatileHeader.
//
// Why this exists as a separate main package rather than a flag on
// the production binary: man-page generation needs cobra/doc, a
// dependency the production binary doesn't otherwise use. Splitting
// keeps the production binary at its 6 MB budget (per
// rules/performance.md) and confines the cobra/doc dependency to a
// dev-only artefact.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra/doc"
	"github.com/vail130/distill-ai/internal/cli"
)

func main() {
	outDir := flag.String("o", "man", "output directory; man/man1/ will be created underneath")
	flag.Parse()
	if err := generate(*outDir); err != nil {
		fmt.Fprintf(os.Stderr, "gen-man: %v\n", err)
		os.Exit(1)
	}
}

// generate walks the cobra tree and writes one .1 file per command
// into outDir/man1/. The directory is created if missing.
//
// Each generated page is post-processed by stripVolatileHeader to
// remove the auto-injected date so re-running the generator on the
// same source SHA produces byte-identical output. Without that, CI
// would re-touch every page on every run and the drift-guard tests
// would chase a moving target.
func generate(outDir string) error {
	dest := filepath.Join(outDir, "man1")
	if err := os.MkdirAll(dest, 0o755); err != nil { //nolint:gosec // man dirs are world-readable by convention
		return fmt.Errorf("create %s: %w", dest, err)
	}
	// Build the cobra tree exactly as the production binary does
	// so the generated pages reflect every flag and subcommand
	// the user actually sees. The streams are discarded — they
	// matter only at execute time, not at documentation time.
	root := cli.NewRootCmd(strings.NewReader(""), io.Discard, io.Discard)
	header := &doc.GenManHeader{
		Title:   "DISTILL-AI",
		Section: "1",
		// The Source / Manual fields are intentionally left
		// blank: cobra/doc inserts a date on every run if they
		// are populated and unset values render as a single
		// stable line we strip below. The Source / Manual
		// fields would otherwise have to be re-bumped on every
		// release, defeating the goal of byte-identical output
		// across regenerations on the same source SHA.
	}
	if err := doc.GenManTree(root, header, dest); err != nil {
		return fmt.Errorf("GenManTree: %w", err)
	}
	// Strip the volatile date line from every generated file.
	// Done in a second pass so the cobra/doc machinery doesn't
	// have to know about it; if cobra ever adds a "no date"
	// option, we can drop this pass.
	entries, err := os.ReadDir(dest)
	if err != nil {
		return fmt.Errorf("read %s: %w", dest, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".1") {
			continue
		}
		path := filepath.Join(dest, e.Name())
		if err := stripVolatileHeader(path); err != nil {
			return fmt.Errorf("strip %s: %w", path, err)
		}
	}
	return nil
}

// volatileDateLine matches the `.TH "NAME" "SECTION" "DATE" ...`
// roff header cobra/doc emits. The DATE field defaults to today's
// date in `Mon YYYY` form, which would re-touch every man page on
// every regeneration. We normalise it to a stable placeholder so
// the files are byte-identical across runs.
//
// The roff syntax is positional; we rewrite the third quoted field
// only and leave the rest of the header untouched. Tested by
// TestManpageGeneration_Deterministic.
var volatileDateLine = regexp.MustCompile(`^(\.TH "[^"]+" "[^"]+") "[^"]*"(.*)$`)

// stripVolatileHeader rewrites the .TH header's date field in-place
// so the generated man page is stable across regenerations on the
// same source SHA.
func stripVolatileHeader(path string) error {
	raw, err := os.ReadFile(path) //nolint:gosec // path comes from os.ReadDir over our own generated output
	if err != nil {
		return err
	}
	lines := bytes.Split(raw, []byte{'\n'})
	for i, line := range lines {
		// Only the .TH header line carries the date. Stopping
		// after the first match keeps the pass linear.
		if m := volatileDateLine.FindSubmatch(line); m != nil {
			lines[i] = append(append([]byte{}, m[1]...), append([]byte(" \"\""), m[2]...)...)
			break
		}
	}
	out := bytes.Join(lines, []byte{'\n'})
	return os.WriteFile(path, out, 0o644) //nolint:gosec // man pages must be world-readable
}
