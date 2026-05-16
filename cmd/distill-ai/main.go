// Command distill-ai distills noisy command output (test runs,
// application logs, stack traces) into a compact, structured form for
// LLM consumption.
//
// See ARCHITECTURE.md for the design and the README for usage.
package main

import (
	"fmt"
	"io"
	"os"
)

// Build-time variables, set via -ldflags by the Makefile and goreleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	code := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	os.Exit(code)
}

// run is main's testable core. It parses args, dispatches to the
// appropriate subcommand, and returns the process exit code. stdin,
// stdout, and stderr are passed in so tests can substitute buffers.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// Top-level flags consume early returns before subcommand dispatch.
	for _, a := range args {
		switch a {
		case "-v", "--version":
			fmt.Fprintf(stdout, "distill-ai %s (commit %s, built %s)\n", version, commit, date)
			return 0
		case "-h", "--help":
			printHelp(stdout)
			return 0
		}
	}
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	// First positional is the subcommand (or the format when
	// piping; M8 wires the full CLI). Today only `detect` is
	// implemented.
	switch args[0] {
	case "detect":
		return cmdDetect(args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "distill-ai: unknown subcommand or flag %q\n", args[0])
		fmt.Fprintln(stderr, "Run 'distill-ai --help' for usage.")
		return 2
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, `distill-ai — compress logs and test output for LLM consumption

Usage:
  distill-ai detect FILE       Identify which format a file is in.

  cmd | distill-ai             (Pipeline mode lands in M8.)

Flags:
  -h, --help     Show this help.
  -v, --version  Show version.

This is an early development build. The full pipeline CLI lands in M8.
See https://github.com/vail130/distill-ai for the roadmap.`)
}
