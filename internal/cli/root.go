package cli

//go:generate go run ../../cmd/distill-ai/gen-man -o ../../man

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newRootCmd returns a fresh root *cobra.Command wired with every
// subcommand the binary supports today. A factory rather than a
// package-level singleton because cobra's Command type carries
// per-invocation state (parsed flags, captured streams) and the test
// suite runs many invocations per process.
//
// stdin/stdout/stderr are injected so tests can substitute buffers;
// production main() passes os.Stdin / os.Stdout / os.Stderr.
func newRootCmd(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "distill-ai",
		Short: "distill-ai — compress logs and test output for LLM consumption",
		Long: `distill-ai is a Unix filter that distills noisy command output
(test runs, application logs, stack traces) into a compact,
structured form suitable for LLM context windows or human triage.

The default invocation (` + "`cmd | distill-ai`" + `) reads from stdin,
autodetects the format, and emits the distilled stream to stdout.
Use the 'run' subcommand explicitly when you want positional FILE
arguments and an explicit FORMAT.

This is an early development build. Several flags are registered but
not yet plumbed; their help text says "(plumbing lands in M8.x)".
See ARCHITECTURE.md and TODO.md for the roadmap.`,
		// Suppress cobra's default behaviours that conflict with
		// the testing seam: we want errors returned, not printed,
		// and we don't want usage printed on every error.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	// Capture streams on the command so subcommands inherit them.
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	// Top-level --version flag. Cobra has a built-in Version field
	// that drives `--version`; we set it to the ldflag-injected
	// format string so the output matches the pre-cobra binary.
	root.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	// Override cobra's default version template so the binary
	// keeps its existing `distill-ai <version>...` shape rather
	// than the cobra default of `distill-ai version <version>`.
	root.SetVersionTemplate("distill-ai {{.Version}}\n")
	// --version is the cobra-managed flag (driven by root.Version).
	// -v moved from --version to --verbose in M8.2; the run
	// subcommand owns the -v binding now. The version flag stays
	// long-form only at the root level.
	root.Flags().Bool("version", false, "Show version.")
	// --config <path> overrides config discovery: when set, only
	// the named TOML file is loaded (no project walk, no user
	// config). Persistent so every subcommand can read it.
	var configPath string
	root.PersistentFlags().StringVar(&configPath, "config", "",
		"Path to a TOML configuration file. Overrides automatic discovery (no project walk, no user config).")
	// Persistent pre-run resolves --config or runs LoadAll, then
	// stores the result on the command's context for subcommands
	// to consume via configFromContext. Failures propagate as
	// exitCodeError so the binary exits with the right code.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		return loadConfigForRoot(cmd, configPath)
	}
	// Hide cobra's auto-generated `completion` subcommand until
	// M8.7 explicitly wires shell completion. Leaving it visible
	// would expose a verb that isn't documented in --help / docs
	// yet, and the integration test's drift guard would flag it.
	root.CompletionOptions.DisableDefaultCmd = true
	runCmd := newRunCmd()
	root.AddCommand(runCmd)
	root.AddCommand(newDetectCmd())
	root.AddCommand(newListFormatsCmd())
	root.AddCommand(newCompletionsCmd())
	root.AddCommand(newExplainCmd())
	root.AddCommand(newVersionCmd())
	// Make run the default subcommand so `cmd | distill-ai` and
	// `distill-ai FORMAT FILE` work without typing `run`. Cobra
	// doesn't have a first-class "default subcommand" concept;
	// the idiom is to wire the root's RunE to the same function
	// the subcommand uses. The flags must also be visible on the
	// root command so users can write `distill-ai --output=json`
	// without `run`. We solve this by replaying the run command's
	// flag set onto the root, sharing the same backing struct.
	mergeFlags(root, runCmd)
	root.RunE = runCmd.RunE
	// Accept arbitrary positional args at the root. Without this,
	// cobra refuses to dispatch to root.RunE when the user types
	// `distill-ai pytest some.log` — it sees "pytest" as an unknown
	// subcommand and bails. cobra.ArbitraryArgs disables the
	// strict-subcommand check; runRun (and splitRunArgs) then
	// decides whether the first positional is a format name or a
	// file path.
	root.Args = cobra.ArbitraryArgs
	return root
}

// mergeFlags copies every flag defined on src onto dst so the user
// can spell `distill-ai --flag` at the root level instead of
// `distill-ai run --flag`. The flag.Flag pointers are shared, which
// means the same backing struct receives parses from either path.
//
// Help text on the root reflects the merged flag set; that's a
// trade-off — the root help is busier — but the common-case
// invocation (`cmd | distill-ai`) needs the flags to work at the
// root level for the design to feel like a Unix filter.
func mergeFlags(dst, src *cobra.Command) {
	src.Flags().VisitAll(func(f *pflag.Flag) {
		if dst.Flags().Lookup(f.Name) != nil {
			return
		}
		dst.Flags().AddFlag(f)
	})
}

// run is the testable entry point. It builds a fresh root command,
// executes it with argv, and translates the result into a process
// exit code per the conventions in ARCHITECTURE.md § Exit codes.
//
// The function signature is preserved from the pre-cobra
// implementation so existing tests keep working. M8.3 will
// formalise the exit-code constants.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRootCmd(stdin, stdout, stderr)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	// A subcommand that returned a *exitCodeError carries its own
	// intended exit code. Anything else is an argument-parsing /
	// command-routing failure, which is exit 2 (errFlagSyntax).
	var ec *exitCodeError
	if errors.As(err, &ec) {
		if ec.message != "" {
			fmt.Fprintf(stderr, "distill-ai: %s\n", ec.message)
		}
		return ec.code
	}
	// Unknown subcommands and unknown flags arrive here as plain
	// error values from cobra. Translate cobra's "unknown command"
	// wording to the pre-cobra "unknown subcommand" so existing
	// tooling (and the project's own docs) keep working. cobra-
	// reported errors are flag-parsing or subcommand-routing
	// failures, both of which map to ExitError.
	msg := err.Error()
	msg = strings.Replace(msg, "unknown command", "unknown subcommand", 1)
	fmt.Fprintf(stderr, "distill-ai: %s\n", msg)
	fmt.Fprintln(stderr, "Run 'distill-ai --help' for usage.")
	return ExitError
}

// exitCodeError lets a subcommand request a specific process exit
// code while still using cobra's error-return idiom. M8.3 will
// rename these and add constants for the four documented codes
// (0/1/2/3); for now the type carries enough to preserve the
// pre-cobra detect-subcommand exit-code surface.
type exitCodeError struct {
	code    int
	message string // optional; logged to stderr by run() when non-empty
	cause   error  // optional; for errors.Is/As chaining
}

func (e *exitCodeError) Error() string {
	if e.message != "" {
		return e.message
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return fmt.Sprintf("exit code %d", e.code)
}

func (e *exitCodeError) Unwrap() error { return e.cause }
