package custom_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/custom"
)

// goldenConfig is the TOML shape of a fixture's .config file.
// Mirrors the [[formats.custom.NAME]] block in the project
// configuration, minus the array-table wrapper — every fixture
// declares exactly one custom-format block, so we decode the
// block contents directly.
type goldenConfig struct {
	Name        string `toml:"name"`
	DetectRegex string `toml:"detect_regex"`
	EventStart  string `toml:"event_start"`
	EventEnd    string `toml:"event_end"`
	Severity    string `toml:"severity"`
	Kind        string `toml:"kind"`
}

// TestCustom_Goldens runs every <case>.input through a custom
// Format built from <case>.config and diffs the emitted Events
// against <case>.expected. The goldens harness in
// internal/formats doesn't fit (no per-fixture Format config), so
// this is a custom variant.
func TestCustom_Goldens(t *testing.T) {
	dir := "testdata"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	cases := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".input") {
			cases = append(cases, strings.TrimSuffix(name, ".input"))
		}
	}
	sort.Strings(cases)
	if len(cases) == 0 {
		t.Fatalf("no fixtures under %s", dir)
	}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			cfg := loadGoldenConfig(t, filepath.Join(dir, name+".config"))
			f, err := custom.New(cfg.Name, cfg.DetectRegex, cfg.EventStart, cfg.EventEnd, cfg.Severity, cfg.Kind)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			inputPath := filepath.Join(dir, name+".input")
			expectedPath := filepath.Join(dir, name+".expected")
			input, err := os.ReadFile(inputPath) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("read %s: %v", inputPath, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ch, err := f.Parse(ctx, bytes.NewReader(input), formats.ParseOpts{})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := drain(ch)
			if got == nil {
				got = nil // collapse for JSON
			}
			actual, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			actual = append(actual, '\n')
			if os.Getenv("DISTILL_AI_UPDATE_GOLDENS") == "1" {
				if err := os.WriteFile(expectedPath, actual, 0o644); err != nil { //nolint:gosec // test path
					t.Fatalf("write %s: %v", expectedPath, err)
				}
				t.Logf("updated %s", expectedPath)
				return
			}
			expected, err := os.ReadFile(expectedPath) //nolint:gosec // test path
			if err != nil {
				t.Fatalf("read %s (DISTILL_AI_UPDATE_GOLDENS=1 to create): %v", expectedPath, err)
			}
			if !bytes.Equal(actual, expected) {
				t.Errorf("%s diverged from golden\n--- expected\n%s\n--- got\n%s",
					name, expected, actual)
			}
		})
	}
}

// loadGoldenConfig decodes a <case>.config TOML file.
func loadGoldenConfig(t *testing.T, path string) goldenConfig {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c goldenConfig
	if _, err := toml.Decode(string(data), &c); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if c.Name == "" {
		c.Name = strings.TrimSuffix(filepath.Base(path), ".config")
	}
	return c
}
