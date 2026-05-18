// Package cli implements the distill-ai CLI surface. It is consumed
// by cmd/distill-ai/main.go (the production binary) and
// cmd/distill-ai/gen-man/main.go (the man-page generator). Splitting
// the cobra command tree into an importable package is what makes
// the man-page generator possible without code duplication; the
// generator constructs the same root command the binary uses and
// hands it to cobra/doc.
//
// The package boundary is also a useful seam for tests that want to
// drive the CLI in-process: the existing test suite (relocated from
// cmd/distill-ai/ in M16.1) uses run() directly.
package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// SetBuildInfo populates the build-info package variables that the
// version subcommand and the --version flag report. Called once at
// process start by cmd/distill-ai/main.go from its ldflag-injected
// vars. Safe to call multiple times (the man-page generator also
// calls it so the rendered man page doesn't print "dev" / "none" /
// "unknown" as placeholder build metadata).
func SetBuildInfo(v, c, d string) {
	version = v
	commit = c
	date = d
}

// NewRootCmd returns a fresh root *cobra.Command wired with every
// subcommand the binary supports. It is the documented entry point
// for callers that want the cobra tree (the man-page generator) or
// want to drive the CLI in-process without going through Run.
//
// stdin/stdout/stderr are injected so callers (tests, generators)
// can substitute buffers; production main() passes os.Stdin /
// os.Stdout / os.Stderr.
func NewRootCmd(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	return newRootCmd(stdin, stdout, stderr)
}

// Run is the production entry point. It builds a fresh root command,
// executes it with argv, and translates the result into a process
// exit code per ARCHITECTURE.md § Exit codes. Returns one of
// ExitOK / ExitNoEvents / ExitError / ExitPartial.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return run(args, stdin, stdout, stderr)
}
