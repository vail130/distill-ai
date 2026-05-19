// Command verify cross-checks the numbers in README.md's
// distill-ai-stats markers against fresh stats produced by
// tools/readme-stats. Exits non-zero on drift so `make
// readme-stats-check` is a CI-grade guard.
//
// Marker shape in README.md:
//
//	<!-- distill-ai-stats:NAME -->
//	... arbitrary markdown referencing the numbers ...
//	<!-- /distill-ai-stats:NAME -->
//
// Inside the marker, the verifier looks for each of the four
// integer values somewhere in the block:
//
//	lines_in       (e.g., "21")
//	lines_out      (e.g., "18")
//	tokens_in      (e.g., "236")
//	tokens_out     (e.g., "179")
//
// Word-boundary regex match — "236" matches inside a table cell or
// prose paragraph but not inside "1236" or "2360". A mismatch
// prints the fixture name, the missing value, and exits non-zero.
// The grammar is tolerant — the marker block can hold a table,
// prose, or code; only the four numeric assertions are pinned.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func main() {
	statsPath := flag.String("stats", "", "path to TSV from tools/readme-stats")
	readmePath := flag.String("readme", "README.md", "path to README.md")
	flag.Parse()
	if *statsPath == "" {
		fmt.Fprintln(os.Stderr, "verify: -stats is required")
		os.Exit(2)
	}
	if err := run(*statsPath, *readmePath); err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		os.Exit(1)
	}
}

func run(statsPath, readmePath string) error {
	stats, err := loadStats(statsPath)
	if err != nil {
		return err
	}
	readme, err := os.ReadFile(readmePath) //nolint:gosec // repo-local path
	if err != nil {
		return fmt.Errorf("read readme: %w", err)
	}
	body := string(readme)
	failed := false
	for name, row := range stats {
		block, ok := extractMarker(body, name)
		if !ok {
			// Not every fixture needs a marker block; skip the
			// ones the README doesn't reference. Document the
			// convention loudly enough that future contributors
			// don't expect this to be a hard requirement.
			continue
		}
		for _, check := range []struct {
			value int
			label string
		}{
			{row.linesIn, "lines_in"},
			{row.linesOut, "lines_out"},
			{row.tokensIn, "tokens_in"},
			{row.tokensOut, "tokens_out"},
		} {
			if !containsNumber(block, check.value) {
				fmt.Fprintf(os.Stderr,
					"drift in README marker %q: expected to find the number %d (%s); regenerate with `make readme-stats` and update the marker block\n",
					name, check.value, check.label)
				failed = true
			}
		}
	}
	if failed {
		return fmt.Errorf("README markers are stale")
	}
	return nil
}

type statsRow struct {
	linesIn, linesOut   int
	tokensIn, tokensOut int
}

func loadStats(path string) (map[string]statsRow, error) {
	f, err := os.Open(path) //nolint:gosec // CLI flag
	if err != nil {
		return nil, fmt.Errorf("open stats: %w", err)
	}
	defer func() { _ = f.Close() }()
	out := make(map[string]statsRow)
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			first = false
			continue // skip header
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			return nil, fmt.Errorf("bad TSV row: %q", line)
		}
		var row statsRow
		for i, p := range []*int{&row.linesIn, &row.linesOut, &row.tokensIn, &row.tokensOut} {
			n, err := atoiTrim(fields[i+1])
			if err != nil {
				return nil, fmt.Errorf("parse %s field %d: %w", fields[0], i, err)
			}
			*p = n
		}
		out[fields[0]] = row
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stats: %w", err)
	}
	return out, nil
}

func atoiTrim(s string) (int, error) {
	s = strings.TrimSpace(s)
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

// containsNumber reports whether n appears in body as a standalone
// integer — bounded by non-digit characters on both sides (or the
// string ends). Stops the verifier from matching "236" inside
// "1236" or "2360".
func containsNumber(body string, n int) bool {
	needle := fmt.Sprintf("%d", n)
	pat := regexp.MustCompile(`(^|[^0-9])` + needle + `([^0-9]|$)`)
	return pat.MatchString(body)
}

// extractMarker finds the body between
//
//	<!-- distill-ai-stats:NAME -->
//
// and the matching close marker, returning the substring and true
// on match.
func extractMarker(body, name string) (string, bool) {
	openRe := regexp.MustCompile(`<!--\s*distill-ai-stats:` + regexp.QuoteMeta(name) + `\s*-->`)
	closeRe := regexp.MustCompile(`<!--\s*/distill-ai-stats:` + regexp.QuoteMeta(name) + `\s*-->`)
	openLoc := openRe.FindStringIndex(body)
	if openLoc == nil {
		return "", false
	}
	closeLoc := closeRe.FindStringIndex(body[openLoc[1]:])
	if closeLoc == nil {
		return "", false
	}
	return body[openLoc[1] : openLoc[1]+closeLoc[0]], true
}
