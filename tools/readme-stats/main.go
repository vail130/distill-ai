// Command readme-stats prints before/after counts for each fixture in
// test/integration/testdata/fixtures/. The numbers feed the
// distill-ai-stats markers in README.md.
//
// Per fixture, prints a TSV line:
//
//	NAME LINES_IN LINES_OUT TOKENS_IN TOKENS_OUT
//
// Lines come from `wc -l` (newline count + trailing-partial-line per
// the convention internal/output/LineCounter uses). Tokens come from
// the same heuristic estimator the production binary uses by default,
// so the stat is comparable to what a user would see in the binary's
// JSON summary.
//
// This is a build-time helper, not part of the production binary.
// Lives under tools/ so it doesn't accidentally ship.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vail130/distill-ai/internal/tokens"
)

func main() {
	binPath := flag.String("bin", "./bin/distill-ai", "path to the distill-ai binary")
	fixturesDir := flag.String("fixtures", "test/integration/testdata/fixtures", "directory of *.input fixtures")
	flag.Parse()
	if err := run(*binPath, *fixturesDir, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "readme-stats: %v\n", err)
		os.Exit(1)
	}
}

// run iterates over every *.input fixture in fixturesDir, runs the
// binary against it, and writes a TSV row to w.
func run(binPath, fixturesDir string, w *os.File) error {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		return fmt.Errorf("read fixtures: %w", err)
	}
	var inputs []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".input") {
			inputs = append(inputs, e.Name())
		}
	}
	sort.Strings(inputs)
	fmt.Fprintln(w, "name\tlines_in\tlines_out\ttokens_in\ttokens_out")
	est := tokens.Default()
	for _, name := range inputs {
		path := filepath.Join(fixturesDir, name)
		raw, err := os.ReadFile(path) //nolint:gosec // path is repo-local fixture
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		linesIn := countLines(raw)
		tokensIn := est.Estimate(string(raw))
		out, err := runDistill(binPath, raw)
		if err != nil {
			return fmt.Errorf("distill %s: %w", name, err)
		}
		linesOut := countLines(out)
		tokensOut := est.Estimate(string(out))
		base := strings.TrimSuffix(name, ".input")
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\n", base, linesIn, linesOut, tokensIn, tokensOut)
	}
	return nil
}

// countLines mirrors output.LineCounter.Lines(): one count per
// newline plus one for a trailing partial line.
func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// runDistill pipes raw into the binary and returns the captured
// stdout. Errors from the binary (non-zero exit, write failure)
// propagate so the caller sees them rather than silently using a
// truncated result.
func runDistill(binPath string, raw []byte) ([]byte, error) {
	cmd := exec.Command(binPath) //nolint:gosec // binPath is a CLI flag
	cmd.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// Exit codes 0, 1, and 3 are all "ran and produced output";
	// only treat real failures (2) as an error. Per ARCHITECTURE.md
	// Exit codes, 1 = no events, 3 = budget partial.
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() == 2 {
			return nil, fmt.Errorf("%w: %s", err, stderr.String())
		}
	}
	return stdout.Bytes(), nil
}
