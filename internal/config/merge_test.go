package config_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/config"
	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// ptrBool / ptrInt / ptrString are small literal-pointer helpers
// so test cases declaring per-format overrides stay readable.
func ptrBool(b bool) *bool       { return &b }
func ptrInt(i int) *int          { return &i }
func ptrString(s string) *string { return &s }

// TestMerge_BothNilReturnsEmpty asserts Merge(nil, nil) is safe
// and returns a non-nil zero Config.
func TestMerge_BothNilReturnsEmpty(t *testing.T) {
	got := config.Merge(nil, nil)
	if got == nil {
		t.Fatalf("Merge returned nil")
	}
	if got.DefaultBudget != 0 || got.DefaultOutput != "" {
		t.Errorf("expected zero Config, got %+v", got)
	}
}

// TestMerge_ProjectOverridesUser: when both configs set the same
// key, project wins.
func TestMerge_ProjectOverridesUser(t *testing.T) {
	user := &config.Config{DefaultBudget: 1000}
	project := &config.Config{DefaultBudget: 2000}
	got := config.Merge(user, project)
	if got.DefaultBudget != 2000 {
		t.Errorf("DefaultBudget = %d, want 2000", got.DefaultBudget)
	}
}

// TestMerge_UserFillsGap: a key set only in the user config
// survives the merge.
func TestMerge_UserFillsGap(t *testing.T) {
	user := &config.Config{DefaultOutput: "json"}
	project := &config.Config{}
	got := config.Merge(user, project)
	if got.DefaultOutput != "json" {
		t.Errorf("DefaultOutput = %q, want json", got.DefaultOutput)
	}
}

// TestMerge_PointerFieldsCarryThrough: when only the user config
// sets a pointer field, it survives.
func TestMerge_PointerFieldsCarryThrough(t *testing.T) {
	user := &config.Config{
		Formats: map[string]config.FormatConfig{
			"pytest": {KeepWarnings: ptrBool(false)},
		},
	}
	project := &config.Config{}
	got := config.Merge(user, project)
	pytest, ok := got.Formats["pytest"]
	if !ok {
		t.Fatalf("Formats[pytest] missing after merge")
	}
	if pytest.KeepWarnings == nil {
		t.Errorf("KeepWarnings = nil, want non-nil pointer to false")
	} else if *pytest.KeepWarnings {
		t.Errorf("KeepWarnings = true, want false")
	}
}

// TestMerge_PerFormatProjectWins: project's per-format override
// beats user's for the same format.
func TestMerge_PerFormatProjectWins(t *testing.T) {
	user := &config.Config{
		Formats: map[string]config.FormatConfig{
			"pytest": {DedupeWindow: ptrInt(100)},
		},
	}
	project := &config.Config{
		Formats: map[string]config.FormatConfig{
			"pytest": {DedupeWindow: ptrInt(200)},
		},
	}
	got := config.Merge(user, project)
	pytest := got.Formats["pytest"]
	if pytest.DedupeWindow == nil || *pytest.DedupeWindow != 200 {
		t.Errorf("DedupeWindow = %v, want pointer to 200", pytest.DedupeWindow)
	}
}

// TestMerge_PerFormatFieldsMerge: project sets some fields, user
// sets others; the merged FormatConfig carries both.
func TestMerge_PerFormatFieldsMerge(t *testing.T) {
	user := &config.Config{
		Formats: map[string]config.FormatConfig{
			"gotest": {ContextLines: ptrInt(5)},
		},
	}
	project := &config.Config{
		Formats: map[string]config.FormatConfig{
			"gotest": {DedupeWindow: ptrInt(100)},
		},
	}
	got := config.Merge(user, project)
	gotest := got.Formats["gotest"]
	if gotest.ContextLines == nil || *gotest.ContextLines != 5 {
		t.Errorf("ContextLines = %v, want pointer to 5", gotest.ContextLines)
	}
	if gotest.DedupeWindow == nil || *gotest.DedupeWindow != 100 {
		t.Errorf("DedupeWindow = %v, want pointer to 100", gotest.DedupeWindow)
	}
}

// TestMerge_DistinctFormatsCoexist: user defines one format,
// project another; both survive.
func TestMerge_DistinctFormatsCoexist(t *testing.T) {
	user := &config.Config{
		Formats: map[string]config.FormatConfig{
			"pytest": {KeepWarnings: ptrBool(true)},
		},
	}
	project := &config.Config{
		Formats: map[string]config.FormatConfig{
			"gotest": {DedupeWindow: ptrInt(100)},
		},
	}
	got := config.Merge(user, project)
	if _, ok := got.Formats["pytest"]; !ok {
		t.Errorf("Formats[pytest] missing")
	}
	if _, ok := got.Formats["gotest"]; !ok {
		t.Errorf("Formats[gotest] missing")
	}
}

// TestMerge_CustomFormatsReplaceWholeBlock: project's
// [[formats.custom.foo]] replaces user's [[formats.custom.foo]]
// outright (no field-level merge for custom blocks).
func TestMerge_CustomFormatsReplaceWholeBlock(t *testing.T) {
	user := &config.Config{
		CustomFormats: map[string]config.CustomFormatConfig{
			"app": {DetectRegex: "user-re", EventStart: "user-start", Severity: "warn"},
		},
	}
	project := &config.Config{
		CustomFormats: map[string]config.CustomFormatConfig{
			"app": {DetectRegex: "proj-re", EventStart: "proj-start"},
		},
	}
	got := config.Merge(user, project)
	custom := got.CustomFormats["app"]
	if custom.DetectRegex != "proj-re" {
		t.Errorf("DetectRegex = %q, want proj-re", custom.DetectRegex)
	}
	// user's Severity must NOT leak through field-level merge.
	if custom.Severity != "" {
		t.Errorf("Severity = %q, want empty (whole-block replace)", custom.Severity)
	}
}

// TestMerge_TopLevelKeepFlagsUnion: KeepWarnings and KeepVendor
// flip only one way — true wins, false never overrides. A user
// who wants to force false must use a per-format pointer.
func TestMerge_TopLevelKeepFlagsUnion(t *testing.T) {
	user := &config.Config{KeepWarnings: true}
	project := &config.Config{KeepWarnings: false}
	got := config.Merge(user, project)
	if !got.KeepWarnings {
		t.Errorf("KeepWarnings = false, want true (union)")
	}
}

// TestMerge_PassthroughPointerPropagates: a *bool pointer at the
// top level distinguishes set-to-false from absent.
func TestMerge_PassthroughPointerPropagates(t *testing.T) {
	user := &config.Config{Passthrough: ptrBool(true)}
	project := &config.Config{Passthrough: ptrBool(false)}
	got := config.Merge(user, project)
	if got.Passthrough == nil {
		t.Fatalf("Passthrough = nil after merge")
	}
	if *got.Passthrough {
		t.Errorf("Passthrough = true, want false (project wins)")
	}
}

// TestApplyToOptions_PerFormatBeatsTopLevel: a per-format block's
// override wins over the top-level value when the active format
// matches.
func TestApplyToOptions_PerFormatBeatsTopLevel(t *testing.T) {
	cfg := &config.Config{
		DedupeWindow: 100,
		Formats: map[string]config.FormatConfig{
			"pytest": {DedupeWindow: ptrInt(200)},
		},
	}
	var opts pipeline.Options
	var po formats.ParseOpts
	cfg.ApplyToOptions(&opts, &po, "pytest")
	if opts.DedupeWindow != 200 {
		t.Errorf("DedupeWindow = %d, want 200 (per-format wins)", opts.DedupeWindow)
	}
	var opts2 pipeline.Options
	var po2 formats.ParseOpts
	cfg.ApplyToOptions(&opts2, &po2, "gotest")
	if opts2.DedupeWindow != 100 {
		t.Errorf("DedupeWindow = %d, want 100 (top-level fallback for gotest)", opts2.DedupeWindow)
	}
}

// TestApplyToOptions_ZeroValueDistinguishedFromUnset: an explicit
// dedupe_window = 0 on a per-format block disables dedupe even
// when the top-level value is non-zero.
func TestApplyToOptions_ZeroValueDistinguishedFromUnset(t *testing.T) {
	cfg := &config.Config{
		DedupeWindow: 500,
		Formats: map[string]config.FormatConfig{
			"pytest": {DedupeWindow: ptrInt(0)},
		},
	}
	var opts pipeline.Options
	var po formats.ParseOpts
	cfg.ApplyToOptions(&opts, &po, "pytest")
	if opts.DedupeWindow != 0 {
		t.Errorf("DedupeWindow = %d, want 0 (explicit per-format zero)", opts.DedupeWindow)
	}
}

// TestApplyToOptions_UnsetFallsThroughToDefault: a config that
// doesn't set a key leaves the caller's pre-populated default in
// place.
func TestApplyToOptions_UnsetFallsThroughToDefault(t *testing.T) {
	cfg := &config.Config{}
	opts := pipeline.Options{Budget: 999}
	po := formats.ParseOpts{}
	cfg.ApplyToOptions(&opts, &po, "")
	if opts.Budget != 999 {
		t.Errorf("Budget = %d, want 999 (config unset)", opts.Budget)
	}
}

// TestApplyToOptions_NilConfigIsNoop: a nil config does not
// modify the caller's options. The function must tolerate the
// nil receiver gracefully.
func TestApplyToOptions_NilConfigIsNoop(t *testing.T) {
	var cfg *config.Config
	opts := pipeline.Options{Budget: 100, Tokenizer: "tiktoken"}
	po := formats.ParseOpts{ContextLines: 5}
	cfg.ApplyToOptions(&opts, &po, "pytest")
	if opts.Budget != 100 || opts.Tokenizer != "tiktoken" || po.ContextLines != 5 {
		t.Errorf("nil-config ApplyToOptions modified opts: %+v / %+v", opts, po)
	}
}

// TestApplyToOptions_MinSeverityPerFormat: a per-format
// min_severity flows into ParseOpts.MinSeverity.
func TestApplyToOptions_MinSeverityPerFormat(t *testing.T) {
	cfg := &config.Config{
		Formats: map[string]config.FormatConfig{
			"pytest": {MinSeverity: ptrString("warn")},
		},
	}
	var opts pipeline.Options
	var po formats.ParseOpts
	cfg.ApplyToOptions(&opts, &po, "pytest")
	if po.MinSeverity != event.SeverityWarn {
		t.Errorf("MinSeverity = %v, want SeverityWarn", po.MinSeverity)
	}
}

// TestApplyToOptions_InvalidMinSeverityIgnored: an invalid
// min_severity string silently falls through. The CLI is
// expected to validate before reaching ApplyToOptions.
func TestApplyToOptions_InvalidMinSeverityIgnored(t *testing.T) {
	cfg := &config.Config{
		Formats: map[string]config.FormatConfig{
			"pytest": {MinSeverity: ptrString("nonsense")},
		},
	}
	var opts pipeline.Options
	po := formats.ParseOpts{MinSeverity: event.SeverityError}
	cfg.ApplyToOptions(&opts, &po, "pytest")
	if po.MinSeverity != event.SeverityError {
		t.Errorf("MinSeverity = %v, want unchanged SeverityError", po.MinSeverity)
	}
}

// TestApplyToOptions_TopLevelOnlyWhenFormatNameEmpty: per-format
// overrides do not apply when formatName is empty.
func TestApplyToOptions_TopLevelOnlyWhenFormatNameEmpty(t *testing.T) {
	cfg := &config.Config{
		DedupeWindow: 100,
		Formats: map[string]config.FormatConfig{
			"pytest": {DedupeWindow: ptrInt(200)},
		},
	}
	var opts pipeline.Options
	var po formats.ParseOpts
	cfg.ApplyToOptions(&opts, &po, "")
	if opts.DedupeWindow != 100 {
		t.Errorf("DedupeWindow = %d, want 100 (top-level only)", opts.DedupeWindow)
	}
}

// TestApplyToOptions_BudgetFromConfig: default_budget seeds
// pipeline.Options.Budget when the caller's default is zero.
func TestApplyToOptions_BudgetFromConfig(t *testing.T) {
	cfg := &config.Config{DefaultBudget: 1500}
	var opts pipeline.Options
	var po formats.ParseOpts
	cfg.ApplyToOptions(&opts, &po, "")
	if opts.Budget != 1500 {
		t.Errorf("Budget = %d, want 1500", opts.Budget)
	}
}

// TestApplyToOptions_CallerBudgetWins: a non-zero opts.Budget
// from the caller (representing an explicit CLI flag) is not
// overwritten by the config's default_budget. CLI > config.
func TestApplyToOptions_CallerBudgetWins(t *testing.T) {
	cfg := &config.Config{DefaultBudget: 1500}
	opts := pipeline.Options{Budget: 99}
	var po formats.ParseOpts
	cfg.ApplyToOptions(&opts, &po, "")
	if opts.Budget != 99 {
		t.Errorf("Budget = %d, want 99 (caller wins)", opts.Budget)
	}
}
