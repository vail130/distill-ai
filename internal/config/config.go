// Package config decodes the optional .distill-ai.toml project
// config and ~/.config/distill-ai/config.toml user config that the
// CLI consults for flag defaults. The shipped schema is documented
// in docs/config.md and sketched in
// ARCHITECTURE.md § Config file.
//
// M14.1 lands only the in-memory Config type and the single-file
// Load function. Discovery (CWD-upward walk, XDG / home fallback)
// is M14.2, merge + precedence is M14.3, CLI wiring is M14.4, and
// custom-format registration via [[formats.custom.NAME]] is M14.5.
// MaxEvents / Passthrough plumbing lands in M14.6 alongside
// resolving KNOWN_ISSUES.md § 1.
//
// Pointer fields on FormatConfig distinguish "the user explicitly
// set this to its zero value" from "the user said nothing." The
// CLI's flag-default resolution treats the two cases differently:
// an explicit zero overrides; an absent key falls through to the
// built-in default. See M14.3 for the merge semantics.
package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the only config-file schema version this binary
// understands. A config that declares a different schema_version
// fails Load with a clear error. The version is intentionally
// distinct from the binary's own version (a config compatible with
// distill-ai v1.0 must also work with v1.1 unless this constant
// changes); the
// [output-stability rule](../../rules/output-stability.md)
// documents the same principle for the JSON-output schema.
//
// Additive keys (new optional fields with safe zero defaults) do
// not bump this constant. Removing or renaming a key requires a
// bump and a deprecation period; the deprecation policy is a v1.x
// decision deferred until a real renaming need exists.
const SchemaVersion = 1

// Config is the decoded TOML configuration. Every field is
// optional; zero values mean "the user did not set this key."
//
// FormatConfig and CustomFormatConfig (defined below) describe the
// per-format sections [formats.<name>] and the custom-format array
// tables [[formats.custom.<name>]]. Per-format values override
// top-level values when the active format matches; see M14.3 for
// the merge rules.
type Config struct {
	// SchemaVersionField is the schema_version key in TOML. The
	// Go field name is suffixed with "Field" to avoid colliding
	// with the package-level SchemaVersion constant. Zero means
	// "the user did not declare a version"; Load treats that as
	// equivalent to SchemaVersion.
	SchemaVersionField int `toml:"schema_version"`

	// DefaultBudget seeds --budget when unset on the CLI. Zero
	// disables budget enforcement (the binary's built-in default).
	DefaultBudget int `toml:"default_budget"`

	// DefaultOutput seeds --output. Valid values are the strings
	// the CLI accepts ("text", "json", "json-streaming",
	// "markdown"); validation happens at flag-parse time, not
	// here, so a config that names an unsupported output produces
	// a CLI error rather than a config-load error.
	DefaultOutput string `toml:"default_output"`

	// DefaultTokenizer seeds --tokenizer. Valid values:
	// "heuristic" (default), "tiktoken". Validation deferred to
	// tokens.ByName at run time.
	DefaultTokenizer string `toml:"default_tokenizer"`

	// DefaultStripEnvelope seeds --strip-envelope. Valid values:
	// "auto" (default), "none", or a registered envelope name
	// ("github-actions", "gitlab-ci"). Validation deferred to
	// envelope.ByName at run time.
	DefaultStripEnvelope string `toml:"default_strip_envelope"`

	// MaxEvents seeds --max-events. Zero (the default) means no
	// cap. M14.6 wires this through to pipeline.Options.MaxEvents
	// and the new MaxEventsStage.
	MaxEvents int `toml:"max_events"`

	// KeepWarnings seeds --keep-warnings. False by default.
	KeepWarnings bool `toml:"keep_warnings"`

	// KeepVendor seeds --keep-vendor. False by default.
	KeepVendor bool `toml:"keep_vendor"`

	// DedupeWindow seeds --dedupe-window. Zero disables dedupe.
	DedupeWindow int `toml:"dedupe_window"`

	// ContextLines seeds --context. Zero means "format default"
	// (3 for the generic format).
	ContextLines int `toml:"context_lines"`

	// Passthrough seeds --passthrough. A pointer because the
	// boolean's zero value is itself meaningful: an explicit
	// passthrough = false in a project config must override an
	// inherited true from the user config. M14.6 wires this
	// through and resolves KNOWN_ISSUES.md § 1.
	Passthrough *bool `toml:"passthrough"`

	// Formats holds the per-format override blocks [formats.NAME].
	// The map key is the lowercase format name as reported by
	// formats.Format.Name() (e.g. "pytest", "gotest", "jest",
	// "generic"). Unknown format names are not rejected at Load
	// time — a future format may have configured options before
	// the format itself ships. The CLI flags this case at run
	// time when the user opts into the unknown format.
	Formats map[string]FormatConfig `toml:"formats"`

	// CustomFormats holds the [[formats.custom.NAME]] array
	// tables. Each entry registers a regex-driven Format at
	// process start; M14.5 owns compilation and registration.
	// The map key is the human-chosen NAME (e.g. "myapp"); the
	// registered format's Name() returns "custom:NAME" so it
	// can't collide with a built-in format of the same name.
	//
	// The TOML key is formats.custom, nested under the same
	// formats table that holds per-format overrides. BurntSushi
	// decodes [[formats.custom.foo]] into
	// CustomFormats["foo"] = CustomFormatConfig{...}.
	CustomFormats map[string]CustomFormatConfig `toml:"-"`
}

// FormatConfig holds the per-format overrides for one
// [formats.NAME] block. Every field that has a top-level Config
// counterpart appears here as a pointer so an absent key falls
// through to the top-level value (and ultimately the built-in
// default), while an explicit zero overrides.
//
// Adding a new field: add the pointer here, the corresponding
// non-pointer field on Config, and the merge rule in M14.3. The
// pointer-vs-zero distinction is the single subtle thing to
// remember about this struct.
type FormatConfig struct {
	// KeepWarnings overrides the top-level keep_warnings for
	// this format.
	KeepWarnings *bool `toml:"keep_warnings"`

	// KeepVendor overrides the top-level keep_vendor for this
	// format.
	KeepVendor *bool `toml:"keep_vendor"`

	// DedupeWindow overrides the top-level dedupe_window. A
	// pointer to int with value 0 explicitly disables dedupe
	// for this format even when the top-level value is non-zero.
	DedupeWindow *int `toml:"dedupe_window"`

	// ContextLines overrides the top-level context_lines for
	// this format.
	ContextLines *int `toml:"context_lines"`

	// MinSeverity overrides the parser's default minimum
	// severity. Valid values are the strings event.ParseSeverity
	// accepts ("error", "warn", "info"). Validation deferred to
	// the consuming format at run time.
	MinSeverity *string `toml:"min_severity"`
}

// CustomFormatConfig describes one [[formats.custom.NAME]] block.
// All regex bodies are kept as strings here; compilation and
// validation are the responsibility of M14.5's
// internal/formats/custom package. Keeping regex compilation out
// of Load makes Load cheap (a stub binary that only Loads a config
// for inspection does not need to compile regexes) and keeps the
// error reporting on regex compilation close to the code that
// uses the result.
type CustomFormatConfig struct {
	// DetectRegex matches at least one line of the input sample
	// to claim the format with Confidence == 1.0. Required.
	DetectRegex string `toml:"detect_regex"`

	// EventStart marks the first line of an Event. Required.
	EventStart string `toml:"event_start"`

	// EventEnd marks the last line of an Event. Optional; when
	// empty, each EventStart match becomes a one-line Event.
	EventEnd string `toml:"event_end"`

	// Severity defaults to "error" when empty. Validation
	// deferred to M14.5.
	Severity string `toml:"severity"`

	// Kind defaults to "match" when empty. M14.5's SCHEMA.md
	// note documents that custom-format kinds are open-set
	// (the user picks the string).
	Kind string `toml:"kind"`
}

// errUnknownKeys collects unknown top-level and nested keys so
// Load can report every typo in one error rather than fail-fast
// on the first one.
var errUnknownKeys = errors.New("unknown configuration keys")

// LoadError wraps the underlying load failure with the offending
// path so callers can produce a clear "config at /path/x.toml is
// invalid" message without re-deriving the path. The error chain
// (via errors.Unwrap) preserves the original TOML decode error
// for callers that want to inspect line:column info.
type LoadError struct {
	Path string
	Err  error
}

// Error formats the load failure for human consumption.
func (e *LoadError) Error() string {
	if e.Path == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("config %s: %s", e.Path, e.Err)
}

// Unwrap exposes the underlying error so errors.Is and errors.As
// can match against decode-error sentinels.
func (e *LoadError) Unwrap() error { return e.Err }

// Load decodes one TOML file at path into a Config. A typo (an
// unknown key) is treated as an error: the user wants to know
// "keep_warning" should have been "keep_warnings", not have the
// value silently ignored.
//
// The schema_version key is validated against the package-level
// SchemaVersion constant; mismatches return an error that names
// both versions.
//
// Required fields on CustomFormatConfig (detect_regex,
// event_start) are validated here; missing values return an
// error naming the offending block.
//
// Load does not walk parent directories, does not consult the
// user's home, and does not merge with any other config. M14.2's
// Discover handles discovery; M14.3's Merge handles precedence.
func Load(path string) (*Config, error) {
	// gosec G304: reading a caller-supplied path is the whole
	// purpose of Load. M14.2's Discover narrows the path set to
	// .distill-ai.toml under CWD-or-ancestor and config.toml
	// under XDG / home.
	data, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		return nil, &LoadError{Path: path, Err: err}
	}
	cfg, err := loadBytes(data)
	if err != nil {
		return nil, &LoadError{Path: path, Err: errors.Unwrap(err)}
	}
	return cfg, nil
}

// LoadBytes decodes a TOML document from an in-memory byte slice.
// Useful for tests and for the M14.4 --config flag's "config is
// piped from stdin" form. The Path on any returned LoadError is
// empty.
func LoadBytes(data []byte) (*Config, error) {
	return loadBytes(data)
}

// loadBytes is the shared decode path for Load and LoadBytes. It
// returns a *LoadError so the file-based wrapper can re-stamp the
// path on top.
func loadBytes(data []byte) (*Config, error) {
	var cfg Config
	meta, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, &LoadError{Err: err}
	}
	if err := validateUnknownKeys(meta); err != nil {
		return nil, &LoadError{Err: err}
	}
	if err := validateSchemaVersion(&cfg); err != nil {
		return nil, &LoadError{Err: err}
	}
	if err := decodeCustomFormats(data, &cfg); err != nil {
		return nil, &LoadError{Err: err}
	}
	if err := validateCustomFormats(&cfg); err != nil {
		return nil, &LoadError{Err: err}
	}
	return &cfg, nil
}

// validateUnknownKeys returns an error naming every key the TOML
// decoder did not consume. BurntSushi's MetaData.Undecoded()
// reports the full dotted path of each unknown key, so the error
// includes section context (e.g., "formats.pytest.keep_warning"
// rather than just "keep_warning").
func validateUnknownKeys(meta toml.MetaData) error {
	undecoded := meta.Undecoded()
	if len(undecoded) == 0 {
		return nil
	}
	// "formats.custom.<name>" array tables decode into the
	// CustomFormats map manually (see decodeCustomFormats);
	// their nested keys appear as undecoded here and must be
	// filtered out before we complain.
	var unknown []string
	for _, key := range undecoded {
		s := key.String()
		if strings.HasPrefix(s, "formats.custom.") {
			continue
		}
		unknown = append(unknown, s)
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%w: %s", errUnknownKeys, strings.Join(unknown, ", "))
}

// validateSchemaVersion enforces that a declared schema_version
// matches the binary's expected SchemaVersion constant. A zero
// (unset) value is treated as the current version so unmarked
// configs from earlier in the project lifecycle keep working.
func validateSchemaVersion(cfg *Config) error {
	if cfg.SchemaVersionField == 0 || cfg.SchemaVersionField == SchemaVersion {
		return nil
	}
	return fmt.Errorf(
		"config schema version %d not supported by this binary (version %d)",
		cfg.SchemaVersionField, SchemaVersion,
	)
}

// decodeCustomFormats reconstructs Config.CustomFormats from the
// raw [[formats.custom.NAME]] array tables. BurntSushi's struct
// decoder doesn't natively map array-of-tables under a deeply
// nested key into a map[string]T, so we re-decode into a scratch
// structure and copy across. A successful primary Load means the
// re-decode cannot fail on the same input, so any error here is
// a defect, not a user-visible config error.
func decodeCustomFormats(data []byte, cfg *Config) error {
	type scratch struct {
		Formats struct {
			Custom map[string][]CustomFormatConfig `toml:"custom"`
		} `toml:"formats"`
	}
	var s scratch
	if _, err := toml.Decode(string(data), &s); err != nil {
		return fmt.Errorf("custom-format re-decode: %w", err)
	}
	if len(s.Formats.Custom) == 0 {
		return nil
	}
	out := make(map[string]CustomFormatConfig, len(s.Formats.Custom))
	for name, entries := range s.Formats.Custom {
		// [[formats.custom.NAME]] is array-of-tables in TOML
		// but a single block is the common case. Accept any
		// number; take the last so a user appending a second
		// block can override an earlier one. A future revision
		// could promote this to an error or to a slice if a
		// real use case appears.
		if len(entries) == 0 {
			continue
		}
		out[name] = entries[len(entries)-1]
	}
	cfg.CustomFormats = out
	return nil
}

// validateCustomFormats enforces the required-field invariants
// for each [[formats.custom.NAME]] block. The errors name the
// block and field so users can locate the problem in their
// config without scanning the whole file.
func validateCustomFormats(cfg *Config) error {
	if len(cfg.CustomFormats) == 0 {
		return nil
	}
	names := make([]string, 0, len(cfg.CustomFormats))
	for name := range cfg.CustomFormats {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := cfg.CustomFormats[name]
		if c.DetectRegex == "" {
			return fmt.Errorf("formats.custom.%s: detect_regex is required", name)
		}
		if c.EventStart == "" {
			return fmt.Errorf("formats.custom.%s: event_start is required", name)
		}
	}
	return nil
}
