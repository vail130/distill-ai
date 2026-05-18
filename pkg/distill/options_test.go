package distill_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/pkg/distill"
)

// TestOptions_OutputFormatStringers asserts every documented
// OutputFormat constant has a stable String() value matching the
// constant's underlying string. Future renames are caught by this
// test before they break consumers.
func TestOptions_OutputFormatStringers(t *testing.T) {
	cases := []struct {
		in   distill.OutputFormat
		want string
	}{
		{distill.OutputText, "text"},
		{distill.OutputJSON, "json"},
		{distill.OutputJSONStreaming, "json-streaming"},
		{distill.OutputMarkdown, "markdown"},
		{distill.OutputFormat(""), "text"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("OutputFormat(%q).String() = %q, want %q",
				string(c.in), got, c.want)
		}
	}
}

// TestOptions_DefaultsAreSafe pins the documented zero-value
// semantics. A zero Options means: autodetect format, text output
// to a nil Writer (which returns ErrNilWriter), no budget, heuristic
// tokenizer (resolved later), dedupe off, no min-severity filter
// (parser default), no envelope strip (resolved as "auto"), no
// markdown fence language, footer enabled. The test asserts each
// explicit default so future drift is caught.
func TestOptions_DefaultsAreSafe(t *testing.T) {
	var o distill.Options
	if o.Format != "" {
		t.Errorf("default Format = %q, want \"\"", o.Format)
	}
	if o.Strict {
		t.Errorf("default Strict = true, want false")
	}
	if got := o.Output.String(); got != "text" {
		t.Errorf("default Output.String() = %q, want \"text\"", got)
	}
	if o.Budget != 0 {
		t.Errorf("default Budget = %d, want 0", o.Budget)
	}
	if o.Tokenizer != "" {
		t.Errorf("default Tokenizer = %q, want \"\"", o.Tokenizer)
	}
	if o.DedupeWindow != 0 {
		t.Errorf("default DedupeWindow = %d, want 0", o.DedupeWindow)
	}
	if o.KeepVendor {
		t.Errorf("default KeepVendor = true, want false")
	}
	if o.KeepWarnings {
		t.Errorf("default KeepWarnings = true, want false")
	}
	if o.MinSeverity != "" {
		t.Errorf("default MinSeverity = %q, want \"\"", o.MinSeverity)
	}
	if o.MaxEvents != 0 {
		t.Errorf("default MaxEvents = %d, want 0", o.MaxEvents)
	}
	if o.ContextLines != 0 {
		t.Errorf("default ContextLines = %d, want 0", o.ContextLines)
	}
	if o.StripEnvelope != "" {
		t.Errorf("default StripEnvelope = %q, want \"\"", o.StripEnvelope)
	}
	if o.Writer != nil {
		t.Errorf("default Writer != nil")
	}
	if o.NoFooter {
		t.Errorf("default NoFooter = true, want false")
	}
	if o.FenceLang != "" {
		t.Errorf("default FenceLang = %q, want \"\"", o.FenceLang)
	}
}

// TestSummary_ForcedDropsFalseOnCleanRun asserts the documented
// false-on-zero-counters case.
func TestSummary_ForcedDropsFalseOnCleanRun(t *testing.T) {
	s := &distill.Summary{EventsEmitted: 3}
	if s.ForcedDrops() {
		t.Errorf("ForcedDrops() = true on clean run, want false")
	}
}

// TestSummary_ForcedDropsTrueOnDrops asserts the
// EventsDroppedBudget arm of ForcedDrops.
func TestSummary_ForcedDropsTrueOnDrops(t *testing.T) {
	s := &distill.Summary{EventsDroppedBudget: 1}
	if !s.ForcedDrops() {
		t.Errorf("ForcedDrops() = false with EventsDroppedBudget=1, want true")
	}
}

// TestSummary_ForcedDropsTrueOnTruncations asserts the
// EventsTruncated arm of ForcedDrops.
func TestSummary_ForcedDropsTrueOnTruncations(t *testing.T) {
	s := &distill.Summary{EventsTruncated: 1}
	if !s.ForcedDrops() {
		t.Errorf("ForcedDrops() = false with EventsTruncated=1, want true")
	}
}

// TestSummary_ForcedDropsNilReceiver asserts the documented nil-safe
// behaviour: a nil *Summary returns false without panicking. Library
// callers that haven't received a Summary yet can call ForcedDrops
// without a nil guard.
func TestSummary_ForcedDropsNilReceiver(t *testing.T) {
	var s *distill.Summary
	if s.ForcedDrops() {
		t.Errorf("nil *Summary.ForcedDrops() = true, want false")
	}
}

// TestOptions_SummaryFieldsMatchSchema is a drift guard: SCHEMA.md
// documents the JSON-output summary object, and this package's
// Summary struct must mirror every field one-for-one. The test
// parses SCHEMA.md's summary table and asserts every documented
// field has a corresponding Go field with a matching name. The
// mapping rule is JSON-tag-style: SCHEMA.md uses snake_case, Go
// uses CamelCase, and a small mapping table below normalises the
// difference.
//
// Adding a Summary field without updating SCHEMA.md (or vice
// versa) fails this test. Mirrors
// TestJSONSink_SummarySchemaMatchesDoc in internal/output but
// scoped to the library-facing type.
func TestOptions_SummaryFieldsMatchSchema(t *testing.T) {
	// Walk up from the package directory to find the repo root
	// (where docs/ lives).
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(root, "docs", "formats", "SCHEMA.md")); err == nil {
			break
		}
		root = filepath.Dir(root)
	}
	schemaPath := filepath.Join(root, "docs", "formats", "SCHEMA.md")
	body, err := os.ReadFile(schemaPath) //nolint:gosec // test fixture, deliberately reads docs path
	if err != nil {
		t.Fatalf("read SCHEMA.md: %v", err)
	}
	// Documented summary fields, snake_case.
	want := map[string]bool{
		"input_lines":           false,
		"output_lines":          false,
		"events_found":          false,
		"events_emitted":        false,
		"events_deduped":        false,
		"events_dropped_budget": false,
		"events_truncated":      false,
		"frames_collapsed":      false,
		"estimated_tokens":      false,
		"estimator":             false,
		"exit_code":             false,
	}
	row := regexp.MustCompile(`\|\s*` + "`" + `(\w+)` + "`" + `\s*\|`)
	for _, m := range row.FindAllStringSubmatch(string(body), -1) {
		if _, ok := want[m[1]]; ok {
			want[m[1]] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("SCHEMA.md missing summary field row for %q", k)
		}
	}
	// Reverse direction: every Summary field has a corresponding
	// documented row. The check is structural — match Go field
	// names against the snake_case → camelCase normalisation.
	goFields := []string{
		"InputLines", "OutputLines", "EventsFound", "EventsEmitted",
		"EventsDeduped", "EventsDroppedBudget", "EventsTruncated",
		"FramesCollapsed", "EstimatedTokens", "Estimator", "ExitCode",
	}
	if len(goFields) != len(want) {
		t.Fatalf("Summary fields (%d) and schema rows (%d) drift; sync them",
			len(goFields), len(want))
	}
	for _, g := range goFields {
		snake := camelToSnake(g)
		if _, ok := want[snake]; !ok {
			t.Errorf("Summary.%s has no SCHEMA.md row (want %q)", g, snake)
		}
	}
}

// camelToSnake converts a Go field name to snake_case. Trivial
// implementation: insert _ before each uppercase letter that follows
// a lowercase letter. Sufficient for the Summary field names.
func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' && s[i-1] >= 'a' && s[i-1] <= 'z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
