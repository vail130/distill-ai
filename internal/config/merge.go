package config

import (
	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// Merge combines two loaded configs into one, with project values
// overriding user values for every top-level key and every
// per-format key. Either argument may be nil; Merge(nil, nil)
// returns a non-nil empty *Config so callers don't have to
// nil-check the result.
//
// Precedence: project > user > built-in default. The "built-in
// default" is supplied by the consumer (the CLI's pre-populated
// flag default or the pipeline's zero-value field); Merge knows
// nothing about it. See ApplyToOptions for the flag-default
// resolution that consults built-in defaults.
//
// Pointer fields on FormatConfig carry the "explicit zero vs
// unset" distinction described on the type godoc. A nil pointer in
// project falls through to the user value; a non-nil pointer in
// project wins even if its target value is the zero value of its
// type.
func Merge(user, project *Config) *Config {
	out := &Config{}
	applyConfig(out, user)
	applyConfig(out, project)
	return out
}

// applyConfig copies non-zero / non-nil fields from src onto dst
// in place. Project applies after user so project wins. The
// per-format and custom-format maps merge by key: project's entry
// overrides user's entry for the same name; entries unique to
// either side survive.
func applyConfig(dst, src *Config) {
	if src == nil {
		return
	}
	if src.SchemaVersionField != 0 {
		dst.SchemaVersionField = src.SchemaVersionField
	}
	if src.DefaultBudget != 0 {
		dst.DefaultBudget = src.DefaultBudget
	}
	if src.DefaultOutput != "" {
		dst.DefaultOutput = src.DefaultOutput
	}
	if src.DefaultTokenizer != "" {
		dst.DefaultTokenizer = src.DefaultTokenizer
	}
	if src.DefaultStripEnvelope != "" {
		dst.DefaultStripEnvelope = src.DefaultStripEnvelope
	}
	if src.MaxEvents != 0 {
		dst.MaxEvents = src.MaxEvents
	}
	// KeepWarnings and KeepVendor are bool top-level fields. Their
	// "unset vs explicit false" distinction is intentionally
	// dropped at the top level: a user who wants to force false
	// from a config can use a per-format [formats.NAME] block
	// whose pointer field is non-nil. The top-level keys only
	// flip the default from false → true.
	if src.KeepWarnings {
		dst.KeepWarnings = true
	}
	if src.KeepVendor {
		dst.KeepVendor = true
	}
	if src.DedupeWindow != 0 {
		dst.DedupeWindow = src.DedupeWindow
	}
	if src.ContextLines != 0 {
		dst.ContextLines = src.ContextLines
	}
	if src.Passthrough != nil {
		dst.Passthrough = src.Passthrough
	}
	if len(src.Formats) > 0 {
		if dst.Formats == nil {
			dst.Formats = make(map[string]FormatConfig, len(src.Formats))
		}
		for name, override := range src.Formats {
			dst.Formats[name] = mergeFormatConfig(dst.Formats[name], override)
		}
	}
	if len(src.CustomFormats) > 0 {
		if dst.CustomFormats == nil {
			dst.CustomFormats = make(map[string]CustomFormatConfig, len(src.CustomFormats))
		}
		for name, custom := range src.CustomFormats {
			// Custom formats don't have meaningful field-level
			// merging — the whole block is the configuration. A
			// project's [[formats.custom.foo]] replaces the user's
			// [[formats.custom.foo]] outright. Different names
			// coexist.
			dst.CustomFormats[name] = custom
		}
	}
}

// mergeFormatConfig combines per-format overrides for the same
// format name. Pointer-non-nil wins.
func mergeFormatConfig(dst, src FormatConfig) FormatConfig {
	if src.KeepWarnings != nil {
		dst.KeepWarnings = src.KeepWarnings
	}
	if src.KeepVendor != nil {
		dst.KeepVendor = src.KeepVendor
	}
	if src.DedupeWindow != nil {
		dst.DedupeWindow = src.DedupeWindow
	}
	if src.ContextLines != nil {
		dst.ContextLines = src.ContextLines
	}
	if src.MinSeverity != nil {
		dst.MinSeverity = src.MinSeverity
	}
	return dst
}

// ApplyToOptions writes config-provided values onto a
// pipeline.Options and a formats.ParseOpts, honouring per-format
// overrides for the active format. The CLI calls ApplyToOptions
// before flag parsing so the resolved values become the flag
// defaults; explicit flags then override.
//
// Precedence inside ApplyToOptions: per-format override → top-
// level config key → caller's pre-populated default. The caller's
// defaults are whatever the opts / parseOpts pointer values are at
// call time; a value of zero / empty string / false is treated as
// "use the config or built-in default."
//
// formatName is the registered format name (`pytest`, `gotest`,
// `jest`, `generic`); pass the resolved format's Name() or the
// explicit positional from the CLI. An empty formatName disables
// per-format overrides — only top-level keys apply.
//
// ApplyToOptions returns the active *FormatConfig pointer for the
// named format (or nil) so callers can inspect details that don't
// fit on the Options structs (e.g., MinSeverity on a per-format
// block, which the parser reads via ParseOpts).
func (c *Config) ApplyToOptions(opts *pipeline.Options, parseOpts *formats.ParseOpts, formatName string) *FormatConfig {
	if c == nil {
		return nil
	}
	// Top-level first.
	if c.DedupeWindow != 0 && opts.DedupeWindow == 0 {
		opts.DedupeWindow = c.DedupeWindow
	}
	if c.KeepVendor && !opts.KeepVendor {
		opts.KeepVendor = true
	}
	if c.DefaultBudget != 0 && opts.Budget == 0 {
		opts.Budget = c.DefaultBudget
	}
	if c.DefaultTokenizer != "" && opts.Tokenizer == "" {
		opts.Tokenizer = c.DefaultTokenizer
	}
	if c.ContextLines != 0 && parseOpts.ContextLines == 0 {
		parseOpts.ContextLines = c.ContextLines
	}
	if c.KeepWarnings && !parseOpts.KeepWarnings {
		parseOpts.KeepWarnings = true
	}
	if c.KeepVendor && !parseOpts.KeepVendor {
		parseOpts.KeepVendor = true
	}
	// Per-format overrides.
	if formatName == "" {
		return nil
	}
	fc, ok := c.Formats[formatName]
	if !ok {
		return nil
	}
	if fc.KeepWarnings != nil {
		parseOpts.KeepWarnings = *fc.KeepWarnings
	}
	if fc.KeepVendor != nil {
		parseOpts.KeepVendor = *fc.KeepVendor
		opts.KeepVendor = *fc.KeepVendor
	}
	if fc.DedupeWindow != nil {
		opts.DedupeWindow = *fc.DedupeWindow
	}
	if fc.ContextLines != nil {
		parseOpts.ContextLines = *fc.ContextLines
	}
	if fc.MinSeverity != nil {
		// Best-effort severity parse; invalid values silently
		// fall through to the caller-supplied default. M14.4's
		// CLI integration validates at flag-parse time so the
		// user sees the error before reaching ApplyToOptions.
		if sev, err := event.ParseSeverity(*fc.MinSeverity); err == nil {
			parseOpts.MinSeverity = sev
		}
	}
	return &fc
}
