package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/envelope"
)

// newDetectCmd returns the cobra command for `distill-ai detect FILE`.
// The command identifies which format a file is in and prints a
// human-readable summary: chosen format, confidence, sample size,
// and the runner-up format with its confidence.
//
// Exit codes:
//
//	0  Detection produced a specific format ≥ ConfidenceMinDetect.
//	1  Detection fell back to the generic format, or no format
//	   matched and there is no generic registered.
//	2  Invalid arguments or I/O failure, or --strict was set and no
//	   specific format matched.
func newDetectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detect FILE",
		Short: "Identify which format a file is in.",
		Long: `detect strips any CI / orchestrator envelope, runs the format
autodetector against FILE (or stdin when FILE is "-"), and prints stable
key:value lines describing the chosen envelope, format, confidence, the
sample size consumed, and the runner-up format.

This subcommand exists so users (and tests) can ask "what is this?"
without running a full distillation pipeline.

With --strict, detection fails (exit 2) instead of falling back to
the generic format. Useful in CI where ambiguous input should be a
build break.`,
		// We do our own arg validation so error messages match the
		// pre-cobra wording exactly. cobra.ExactArgs would print
		// "accepts 1 arg(s)" which existing tests don't grep for.
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		RunE:               runDetect,
	}
	cmd.Flags().Bool("strict", false,
		"Fail with exit 2 when no specific format scores above the detection threshold, instead of falling back to generic.")
	return cmd
}

// runDetect is the cobra RunE entry point. It validates arguments,
// opens the input, runs the detector, and prints the result. Errors
// returned from this function carry an embedded exit code so run()
// can translate them.
func runDetect(cmd *cobra.Command, args []string) error {
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()
	stdin := cmd.InOrStdin()
	if len(args) == 0 {
		fmt.Fprintln(stderr, "distill-ai detect: missing FILE argument")
		fmt.Fprintln(stderr, "Usage: distill-ai detect FILE")
		fmt.Fprintln(stderr, "       distill-ai detect -        (read stdin)")
		return &exitCodeError{code: ExitError}
	}
	if len(args) > 1 {
		fmt.Fprintf(stderr, "distill-ai detect: expected exactly one FILE argument, got %d\n", len(args))
		return &exitCodeError{code: ExitError}
	}
	path := args[0]
	r, source, closer, err := openInput(path, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai detect: %v\n", err)
		return &exitCodeError{code: ExitError}
	}
	if closer != nil {
		defer func() { _ = closer.Close() }() // best-effort close; nothing we can do on failure
	}
	strict, _ := cmd.Flags().GetBool("strict")
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	cleaned, signals, stripper, err := envelope.Wrap(ctx, r, envelope.Options{Choice: envelope.ChoiceAuto})
	if err != nil {
		fmt.Fprintf(stderr, "distill-ai: detect %s: %v\n", source, err)
		return &exitCodeError{code: ExitError}
	}
	go func() {
		drained := 0
		for range signals {
			drained++
		}
		_ = drained
	}()
	res, err := detect.Detect(ctx, cleaned, detect.Opts{Strict: strict})
	if err != nil {
		// ErrNoFormat is a "no match" result, not an internal
		// failure. Without --strict it maps to ExitNoEvents (1)
		// with a helpful message; with --strict it maps to
		// ExitError (2) because the caller asked for the
		// build-break semantics.
		if errors.Is(err, detect.ErrNoFormat) {
			fmt.Fprintf(stderr, "distill-ai: no format matched %s\n", source)
			if strict {
				// --strict suppresses the generic fallback;
				// nothing matched and the caller asked for a
				// build-break.
				fmt.Fprintln(stderr, "Hint: no specific format scored above the detection threshold;")
				fmt.Fprintln(stderr, "      --strict suppresses the generic fallback. Remove --strict")
				fmt.Fprintln(stderr, "      to accept low-confidence input as 'generic'.")
				return &exitCodeError{code: ExitError}
			}
			// Without --strict this means the generic fallback
			// itself is missing — the package should be wired in
			// via a side-effect import in main_register.go.
			fmt.Fprintln(stderr, "Hint: no specific format scored above the detection threshold")
			fmt.Fprintln(stderr, "      and the generic fallback is not registered (build misconfiguration).")
			return &exitCodeError{code: ExitNoEvents}
		}
		fmt.Fprintf(stderr, "distill-ai: detect %s: %v\n", source, err)
		return &exitCodeError{code: ExitError}
	}
	printDetectResult(stdout, source, stripper, res)
	if res.FellBackToGeneric {
		return &exitCodeError{code: ExitNoEvents}
	}
	return nil
}

// openInput resolves the FILE argument: '-' reads from stdin, anything
// else opens that path. The returned source is what we print in
// diagnostics; closer is non-nil when the caller must close it.
func openInput(path string, stdin io.Reader) (r io.Reader, source string, closer io.Closer, err error) {
	if path == "-" {
		return stdin, "stdin", nil, nil
	}
	// The path is a user-supplied CLI argument; opening arbitrary
	// user-named files is the entire point of a `detect FILE` command.
	f, err := os.Open(path) //nolint:gosec // G304/G703 path-from-arg is intentional
	if err != nil {
		// os.Open's error already names the path; don't double-name.
		return nil, path, nil, err
	}
	return f, path, f, nil
}

// printDetectResult writes the detector outcome to w in a human-
// readable, parseable shape: stable key: value lines so tests can
// grep / parse without depending on prose.
func printDetectResult(w io.Writer, source string, stripper envelope.Stripper, res *detect.Result) {
	fmt.Fprintf(w, "source: %s\n", source)
	if stripper == nil {
		fmt.Fprintln(w, "envelope: none")
	} else {
		fmt.Fprintf(w, "envelope: %s\n", stripper.Name())
	}
	fmt.Fprintf(w, "format: %s\n", res.Format.Name())
	fmt.Fprintf(w, "confidence: %.2f\n", float64(res.Confidence))
	fmt.Fprintf(w, "sample_bytes: %d\n", len(res.Sample))
	fmt.Fprintf(w, "fellback_to_generic: %t\n", res.FellBackToGeneric)
	if res.Runner != nil {
		fmt.Fprintf(w, "runner: %s\n", res.Runner.Name())
		fmt.Fprintf(w, "runner_confidence: %.2f\n", float64(res.RunnerConfidence))
	} else {
		fmt.Fprintln(w, "runner: (none)")
	}
}
