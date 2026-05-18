package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/vail130/distill-ai/internal/config"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// configContextKey is the context-value key for the loaded merged
// config. Subcommands pull the Config out of cmd.Context() and
// pass it to applyConfigToFlags. A typed key avoids the
// "context.WithValue with a string" lint warning.
type configContextKey struct{}

// loadConfigForRoot is the persistent-pre-run hook the root cobra
// command installs. It resolves --config (if set) or falls back
// to config.LoadAll(cwd, home), and stores the result in the
// command's context so subcommands can read it without
// re-running discovery.
//
// On any load failure the function returns an exitCodeError so
// the binary exits with ExitError (2) and the user sees the
// underlying *LoadError's path-and-cause message.
func loadConfigForRoot(cmd *cobra.Command, configPath string) error {
	var cfg *config.Config
	var err error
	if configPath != "" {
		cfg, err = config.Load(configPath)
	} else {
		cwd, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		cfg, err = config.LoadAll(cwd, home)
	}
	if err != nil {
		return &exitCodeError{code: ExitError, message: err.Error(), cause: err}
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(context.WithValue(ctx, configContextKey{}, cfg))
	return nil
}

// configFromContext retrieves the merged *config.Config stored
// in the cobra command's context by loadConfigForRoot. Returns
// nil when no config was loaded (e.g. the subcommand was invoked
// directly in a test without the persistent pre-run hook); callers
// must treat nil as "no config" gracefully.
func configFromContext(ctx context.Context) *config.Config {
	if ctx == nil {
		return nil
	}
	cfg, _ := ctx.Value(configContextKey{}).(*config.Config)
	return cfg
}

// applyConfigToFlags reads the merged Config from the command's
// context and writes its values onto the run subcommand's
// pipeline.Options / formats.ParseOpts where the corresponding
// CLI flag was NOT explicitly set. The "explicit flag" check uses
// pflag.Flag.Changed, which cobra populates after argument
// parsing.
//
// formatName drives per-format overrides; pass the resolved
// format name (after autodetect, or the explicit positional).
// An empty formatName disables per-format overrides and only the
// top-level keys apply.
//
// applyConfigToFlags returns the resolved per-format block (or
// nil) so callers can read fields that don't fit on the Options
// pair — currently none, but the return is preserved for parity
// with config.Config.ApplyToOptions.
func applyConfigToFlags(
	cmd *cobra.Command,
	opts *pipeline.Options,
	parseOpts *formats.ParseOpts,
	formatName string,
) *config.FormatConfig {
	cfg := configFromContext(cmd.Context())
	if cfg == nil {
		return nil
	}
	// Mask off CLI-set values so ApplyToOptions's "non-zero
	// caller default" treatment leaves them alone. The mask is
	// applied by re-zeroing the opts field if the corresponding
	// flag was NOT Changed — the inverse of "trust the caller".
	//
	// In practice the cobra defaults we registered today are
	// zero values, so the loop is a no-op for unset flags and
	// only prevents ApplyToOptions from overwriting flags the
	// user explicitly passed. The ChangedFlagFlag map maps
	// pflag names → applies the value only when the flag was
	// changed. We codify the rule as:
	//
	//   if flag NOT changed && config sets it: take config.
	//   if flag changed: keep CLI value (non-zero on opts).
	//
	// ApplyToOptions itself implements "take config when caller
	// value is zero", so the precondition holds as long as we
	// don't overwrite an explicit zero from the CLI with a
	// non-zero from config. The "explicit zero" case only
	// matters for --budget=0, --dedupe-window=0, and a few
	// others where 0 is meaningful. We preserve those by NOT
	// resetting opts to zero based on Changed; instead, we
	// short-circuit ApplyToOptions for any field whose flag
	// was explicitly set, by writing a sentinel non-zero value
	// the function won't override. Implementing this without
	// adding new fields:
	//
	//   1. Snapshot the opts/parseOpts BEFORE calling
	//      ApplyToOptions.
	//   2. Call ApplyToOptions to take config defaults.
	//   3. Re-apply the snapshot for every flag the user
	//      Changed, overriding config.
	snapshot := struct {
		opts pipeline.Options
		po   formats.ParseOpts
	}{opts: *opts, po: *parseOpts}
	fc := cfg.ApplyToOptions(opts, parseOpts, formatName)
	flags := cmd.Flags()
	if flagChanged(flags, "budget") {
		opts.Budget = snapshot.opts.Budget
	}
	if flagChanged(flags, "tokenizer") {
		opts.Tokenizer = snapshot.opts.Tokenizer
	}
	if flagChanged(flags, "dedupe") || flagChanged(flags, "no-dedupe") || flagChanged(flags, "dedupe-window") {
		opts.DedupeWindow = snapshot.opts.DedupeWindow
	}
	if flagChanged(flags, "keep-vendor") {
		opts.KeepVendor = snapshot.opts.KeepVendor
		parseOpts.KeepVendor = snapshot.po.KeepVendor
	}
	if flagChanged(flags, "keep-warnings") {
		parseOpts.KeepWarnings = snapshot.po.KeepWarnings
	}
	if flagChanged(flags, "severity") {
		parseOpts.MinSeverity = snapshot.po.MinSeverity
	}
	if flagChanged(flags, "context") {
		parseOpts.ContextLines = snapshot.po.ContextLines
	}
	return fc
}

// flagChanged is a nil-safe wrapper around pflag.FlagSet.Changed
// that returns false when the flag doesn't exist on the set. Used
// by applyConfigToFlags so a typo or a future flag rename produces
// a "config wins" outcome rather than a panic.
func flagChanged(flags *pflag.FlagSet, name string) bool {
	f := flags.Lookup(name)
	if f == nil {
		return false
	}
	return f.Changed
}

// applyOutputConfig applies the config's default_output to a
// flag string when --output was not explicitly set. Separate
// from applyConfigToFlags because the output flag's value flows
// through a string, not through pipeline.Options.
func applyOutputConfig(cmd *cobra.Command, value *string) {
	cfg := configFromContext(cmd.Context())
	if cfg == nil || cfg.DefaultOutput == "" {
		return
	}
	if flagChanged(cmd.Flags(), "output") {
		return
	}
	*value = cfg.DefaultOutput
}

// applyStripEnvelopeConfig applies the config's
// default_strip_envelope to a flag string when --strip-envelope
// was not explicitly set.
func applyStripEnvelopeConfig(cmd *cobra.Command, value *string) {
	cfg := configFromContext(cmd.Context())
	if cfg == nil || cfg.DefaultStripEnvelope == "" {
		return
	}
	if flagChanged(cmd.Flags(), "strip-envelope") {
		return
	}
	*value = cfg.DefaultStripEnvelope
}

// printConfigTOML renders the merged Config as TOML to w. Used
// by --print-config to let users debug "which config is
// winning?" without reverse-engineering the precedence chain.
// Unset fields are omitted via the TOML encoder's default
// behaviour (omitempty-equivalent on zero values), so the output
// is a minimal canonical form of the active configuration.
func printConfigTOML(w io.Writer, cfg *config.Config) error {
	if cfg == nil {
		// Print an empty document so callers can still parse
		// it without special-casing nil.
		_, err := fmt.Fprintln(w, "# (no configuration loaded)")
		return err
	}
	encoder := toml.NewEncoder(w)
	return encoder.Encode(cfg)
}
