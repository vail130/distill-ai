package output

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
)

// updateGoldens, when set, overwrites the *.expected.* files on disk
// with the test's actual output. Run with `go test -update ./...` to
// regenerate fixtures.
var updateGoldens = flag.Bool("update", false, "rewrite golden files")

// goldenCase is one fixture: a list of Events plus the case name. The
// .events.json file under testdata/<encoder>/ is unmarshalled into
// this shape; the per-encoder test runs the Sink on the events and
// diffs the result against the matching .expected.* file.
type goldenCase struct {
	Name   string        `json:"-"`
	Path   string        `json:"-"`
	Events []event.Event `json:"events"`

	// Header inputs the Sink needs to render the footer correctly.
	FormatName    string `json:"format_name"`
	InputLines    int    `json:"input_lines"`
	NoFooter      bool   `json:"no_footer"`
	EstimatorName string `json:"estimator_name"`

	// Streaming and FenceLang are read by the JSON and markdown sinks
	// respectively; ignored by the text sink.
	Streaming bool   `json:"streaming"`
	FenceLang string `json:"fence_lang"`

	// ExitCode is read by the JSON sink only.
	ExitCode int `json:"exit_code"`
}

// loadCases walks testdata/<encoder> and returns one goldenCase per
// .events.json file. The expected output sits alongside as
// .expected.<ext>.
func loadCases(t *testing.T, encoder string) []goldenCase {
	t.Helper()
	dir := filepath.Join("testdata", encoder)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []goldenCase
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".json" || !hasSuffix(name, ".events.json") {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var c goldenCase
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("unmarshal %s: %v", path, err)
		}
		c.Name = name[:len(name)-len(".events.json")]
		c.Path = path
		out = append(out, c)
	}
	if len(out) == 0 {
		t.Fatalf("no goldens found under %s", dir)
	}
	return out
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// goldenCompare diffs got against the .expected.<ext> file matching c
// under testdata/<encoder>. With -update, the expected file is
// rewritten instead.
func goldenCompare(t *testing.T, encoder, ext string, c goldenCase, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", encoder, c.Name+".expected."+ext)
	if *updateGoldens {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("update %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update to create)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s/%s: output mismatch\n--- want ---\n%s\n--- got ---\n%s",
			encoder, c.Name, string(want), string(got))
	}
}
