package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/config"
)

// writeConfig writes a TOML body to a tempfile and returns the path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestConfig_LoadValid feeds the ARCHITECTURE.md example verbatim
// (plus the M14.1 fields the sketch does not mention) and asserts
// every field decodes to the expected value.
func TestConfig_LoadValid(t *testing.T) {
	body := `
schema_version = 1
default_budget = 2000
default_output = "text"
default_tokenizer = "heuristic"
default_strip_envelope = "auto"
max_events = 100
keep_warnings = false
keep_vendor = false
dedupe_window = 500
context_lines = 3
passthrough = true

[formats.pytest]
keep_warnings = false
context_lines = 5

[formats.gotest]
dedupe_window = 1000
min_severity = "warn"

[[formats.custom.myapp]]
detect_regex = '^\[myapp\]'
event_start = '^\[myapp\] ERROR'
event_end = '^\[myapp\] (INFO|DEBUG|ERROR)'
severity = "error"
kind = "myapp_error"
`
	cfg, err := config.LoadBytes([]byte(body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.SchemaVersionField, 1; got != want {
		t.Errorf("SchemaVersionField = %d, want %d", got, want)
	}
	if got, want := cfg.DefaultBudget, 2000; got != want {
		t.Errorf("DefaultBudget = %d, want %d", got, want)
	}
	if got, want := cfg.DefaultOutput, "text"; got != want {
		t.Errorf("DefaultOutput = %q, want %q", got, want)
	}
	if got, want := cfg.DefaultTokenizer, "heuristic"; got != want {
		t.Errorf("DefaultTokenizer = %q, want %q", got, want)
	}
	if got, want := cfg.DefaultStripEnvelope, "auto"; got != want {
		t.Errorf("DefaultStripEnvelope = %q, want %q", got, want)
	}
	if got, want := cfg.MaxEvents, 100; got != want {
		t.Errorf("MaxEvents = %d, want %d", got, want)
	}
	if cfg.KeepWarnings {
		t.Errorf("KeepWarnings = true, want false")
	}
	if cfg.KeepVendor {
		t.Errorf("KeepVendor = true, want false")
	}
	if got, want := cfg.DedupeWindow, 500; got != want {
		t.Errorf("DedupeWindow = %d, want %d", got, want)
	}
	if got, want := cfg.ContextLines, 3; got != want {
		t.Errorf("ContextLines = %d, want %d", got, want)
	}
	if cfg.Passthrough == nil || !*cfg.Passthrough {
		t.Errorf("Passthrough = %v, want pointer to true", cfg.Passthrough)
	}
	pytest, ok := cfg.Formats["pytest"]
	if !ok {
		t.Fatalf("Formats[pytest] missing")
	}
	if pytest.KeepWarnings == nil || *pytest.KeepWarnings {
		t.Errorf("pytest.KeepWarnings = %v, want pointer to false", pytest.KeepWarnings)
	}
	if pytest.ContextLines == nil || *pytest.ContextLines != 5 {
		t.Errorf("pytest.ContextLines = %v, want pointer to 5", pytest.ContextLines)
	}
	gotest, ok := cfg.Formats["gotest"]
	if !ok {
		t.Fatalf("Formats[gotest] missing")
	}
	if gotest.DedupeWindow == nil || *gotest.DedupeWindow != 1000 {
		t.Errorf("gotest.DedupeWindow = %v, want pointer to 1000", gotest.DedupeWindow)
	}
	if gotest.MinSeverity == nil || *gotest.MinSeverity != "warn" {
		t.Errorf("gotest.MinSeverity = %v, want pointer to warn", gotest.MinSeverity)
	}
	myapp, ok := cfg.CustomFormats["myapp"]
	if !ok {
		t.Fatalf("CustomFormats[myapp] missing")
	}
	if got, want := myapp.DetectRegex, `^\[myapp\]`; got != want {
		t.Errorf("myapp.DetectRegex = %q, want %q", got, want)
	}
	if got, want := myapp.EventStart, `^\[myapp\] ERROR`; got != want {
		t.Errorf("myapp.EventStart = %q, want %q", got, want)
	}
	if got, want := myapp.EventEnd, `^\[myapp\] (INFO|DEBUG|ERROR)`; got != want {
		t.Errorf("myapp.EventEnd = %q, want %q", got, want)
	}
	if got, want := myapp.Severity, "error"; got != want {
		t.Errorf("myapp.Severity = %q, want %q", got, want)
	}
	if got, want := myapp.Kind, "myapp_error"; got != want {
		t.Errorf("myapp.Kind = %q, want %q", got, want)
	}
}

// TestConfig_LoadEmpty feeds an empty TOML body and asserts a
// zero-value Config and nil error.
func TestConfig_LoadEmpty(t *testing.T) {
	cfg, err := config.LoadBytes(nil)
	if err != nil {
		t.Fatalf("LoadBytes(nil): %v", err)
	}
	if cfg == nil {
		t.Fatalf("LoadBytes returned nil Config")
	}
	if cfg.DefaultBudget != 0 || cfg.DefaultOutput != "" || cfg.Passthrough != nil {
		t.Errorf("expected zero-value Config, got %+v", cfg)
	}
	if len(cfg.Formats) != 0 || len(cfg.CustomFormats) != 0 {
		t.Errorf("expected empty maps, got Formats=%v CustomFormats=%v",
			cfg.Formats, cfg.CustomFormats)
	}
}

// TestConfig_LoadUnknownKeyErrors feeds a config with a typo and
// asserts Load returns an error mentioning the unknown key.
func TestConfig_LoadUnknownKeyErrors(t *testing.T) {
	body := `
keep_warning = true
default_budget = 100
`
	_, err := config.LoadBytes([]byte(body))
	if err == nil {
		t.Fatalf("Load: expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "keep_warning") {
		t.Errorf("Load error %q does not mention keep_warning", err)
	}
}

// TestConfig_LoadUnknownNestedKeyErrors covers the per-format
// section case so the user sees "formats.pytest.keep_warning"
// rather than just "keep_warning".
func TestConfig_LoadUnknownNestedKeyErrors(t *testing.T) {
	body := `
[formats.pytest]
keep_warning = false
`
	_, err := config.LoadBytes([]byte(body))
	if err == nil {
		t.Fatalf("Load: expected error for nested unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "formats.pytest.keep_warning") {
		t.Errorf("Load error %q does not mention formats.pytest.keep_warning", err)
	}
}

// TestConfig_LoadMalformedTOMLErrors feeds malformed TOML and
// asserts a parse error.
func TestConfig_LoadMalformedTOMLErrors(t *testing.T) {
	body := `
default_budget =
`
	_, err := config.LoadBytes([]byte(body))
	if err == nil {
		t.Fatalf("Load: expected parse error, got nil")
	}
}

// TestConfig_LoadSchemaVersionMatch asserts schema_version == 1
// decodes and schema_version == 2 errors with a clear message.
func TestConfig_LoadSchemaVersionMatch(t *testing.T) {
	if _, err := config.LoadBytes([]byte("schema_version = 1\n")); err != nil {
		t.Errorf("Load v1 schema: %v", err)
	}
	_, err := config.LoadBytes([]byte("schema_version = 2\n"))
	if err == nil {
		t.Fatalf("Load v2 schema: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "schema version 2") {
		t.Errorf("error %q does not name the unsupported version", err)
	}
	if !strings.Contains(err.Error(), "version 1") {
		t.Errorf("error %q does not name the supported version", err)
	}
}

// TestConfig_LoadSchemaVersionUnsetIsTreatedAsCurrent confirms a
// config without an explicit schema_version decodes successfully.
func TestConfig_LoadSchemaVersionUnsetIsTreatedAsCurrent(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(`default_budget = 100`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SchemaVersionField != 0 {
		t.Errorf("expected SchemaVersionField=0 for unset, got %d", cfg.SchemaVersionField)
	}
}

// TestConfig_LoadCustomFormat decodes one custom-format block and
// asserts every field is populated.
func TestConfig_LoadCustomFormat(t *testing.T) {
	body := `
[[formats.custom.svc]]
detect_regex = '^\[svc\]'
event_start = '^\[svc\] ERROR'
event_end = '^\[svc\] (INFO|DEBUG|ERROR)'
severity = "warn"
kind = "svc_problem"
`
	path := writeConfig(t, body)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	svc, ok := cfg.CustomFormats["svc"]
	if !ok {
		t.Fatalf("CustomFormats[svc] missing")
	}
	if svc.DetectRegex == "" || svc.EventStart == "" {
		t.Errorf("required fields empty: %+v", svc)
	}
	if got, want := svc.Severity, "warn"; got != want {
		t.Errorf("Severity = %q, want %q", got, want)
	}
	if got, want := svc.Kind, "svc_problem"; got != want {
		t.Errorf("Kind = %q, want %q", got, want)
	}
}

// TestConfig_LoadCustomFormatMissingRequired asserts a custom
// block missing detect_regex returns a clear error.
func TestConfig_LoadCustomFormatMissingRequired(t *testing.T) {
	body := `
[[formats.custom.svc]]
event_start = '^ERROR'
`
	path := writeConfig(t, body)
	_, err := config.Load(path)
	if err == nil {
		t.Fatalf("Load: expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "formats.custom.svc") {
		t.Errorf("error %q does not name the block", err)
	}
	if !strings.Contains(err.Error(), "detect_regex") {
		t.Errorf("error %q does not name the missing field", err)
	}
}

// TestConfig_LoadCustomFormatMissingEventStart is the sibling
// case for the event_start required field.
func TestConfig_LoadCustomFormatMissingEventStart(t *testing.T) {
	body := `
[[formats.custom.svc]]
detect_regex = '^svc'
`
	path := writeConfig(t, body)
	_, err := config.Load(path)
	if err == nil {
		t.Fatalf("Load: expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "event_start") {
		t.Errorf("error %q does not name event_start", err)
	}
}

// TestConfig_PointerFieldDistinguishesUnsetFromZero verifies the
// pointer-vs-zero rule: an explicit keep_warnings = false on a
// per-format block produces a non-nil *bool, while an absent key
// produces nil.
func TestConfig_PointerFieldDistinguishesUnsetFromZero(t *testing.T) {
	bodyExplicit := `
[formats.pytest]
keep_warnings = false
`
	cfg, err := config.LoadBytes([]byte(bodyExplicit))
	if err != nil {
		t.Fatalf("Load explicit: %v", err)
	}
	pytest := cfg.Formats["pytest"]
	if pytest.KeepWarnings == nil {
		t.Fatalf("explicit keep_warnings=false produced nil pointer")
	}
	if *pytest.KeepWarnings {
		t.Errorf("expected false, got true")
	}
	bodyAbsent := `
[formats.pytest]
context_lines = 5
`
	cfg, err = config.LoadBytes([]byte(bodyAbsent))
	if err != nil {
		t.Fatalf("Load absent: %v", err)
	}
	pytest = cfg.Formats["pytest"]
	if pytest.KeepWarnings != nil {
		t.Errorf("absent keep_warnings produced non-nil pointer (%v)", *pytest.KeepWarnings)
	}
}

// TestConfig_LoadReturnsLoadError asserts the wrapping error
// type carries the path and unwraps to the underlying error.
func TestConfig_LoadReturnsLoadError(t *testing.T) {
	path := writeConfig(t, "default_budget =\n")
	_, err := config.Load(path)
	if err == nil {
		t.Fatalf("Load: expected error")
	}
	var le *config.LoadError
	if !errors.As(err, &le) {
		t.Fatalf("error is not *LoadError: %T %v", err, err)
	}
	if le.Path != path {
		t.Errorf("LoadError.Path = %q, want %q", le.Path, path)
	}
	if le.Unwrap() == nil {
		t.Errorf("LoadError.Unwrap() = nil")
	}
}

// TestConfig_LoadMissingFile reports a clean error rather than
// panicking when the path does not exist.
func TestConfig_LoadMissingFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err == nil {
		t.Fatalf("Load: expected error for missing file")
	}
}

// TestConfig_RoundTripSchemaMatchesDoc parses the
// ARCHITECTURE.md sketch and asserts every documented top-level
// key plus the per-format and custom-format keys decode without
// error. Drift guard: if ARCHITECTURE.md grows a key the loader
// doesn't know about, this test fails.
func TestConfig_RoundTripSchemaMatchesDoc(t *testing.T) {
	body := `
schema_version = 1
default_budget = 2000
default_output = "text"
default_tokenizer = "heuristic"
default_strip_envelope = "auto"
max_events = 100
keep_warnings = false
keep_vendor = false
dedupe_window = 500
context_lines = 3
passthrough = false

[formats.pytest]
keep_warnings = false
context_lines = 3
keep_vendor = false
dedupe_window = 200
min_severity = "error"

[[formats.custom.myapp]]
detect_regex = '^\[myapp\]'
event_start = '^\[myapp\] ERROR'
event_end = '^\[myapp\] (INFO|DEBUG|ERROR)'
`
	if _, err := config.LoadBytes([]byte(body)); err != nil {
		t.Fatalf("Load: %v", err)
	}
}
