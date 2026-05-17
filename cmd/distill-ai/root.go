package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
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

This is an early development build. The full pipeline CLI lands in M8;
today only the 'detect' subcommand is wired. See ARCHITECTURE.md and
TODO.md for the roadmap.`,
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
	// -v is reserved for --verbose in M8.2; for now we wire it as
	// a synonym for --version so the existing CLI surface
	// (-v / --version) keeps working. M8.2 reassigns -v to verbose
	// and the version short-flag goes away.
	root.Flags().BoolP("version", "v", false, "Show version.")
	// Hide cobra's auto-generated `completion` subcommand until
	// M8.7 explicitly wires shell completion. Leaving it visible
	// would expose a verb that isn't documented in --help / docs
	// yet, and the integration test's drift guard would flag it.
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newDetectCmd())
	return root
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
	// tooling (and the project's own docs) keep working.
	msg := err.Error()
	msg = strings.Replace(msg, "unknown command", "unknown subcommand", 1)
	fmt.Fprintf(stderr, "distill-ai: %s\n", msg)
	fmt.Fprintln(stderr, "Run 'distill-ai --help' for usage.")
	return 2
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
