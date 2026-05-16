// Command distill-ai distills noisy command output (test runs, application
// logs, stack traces) into a compact, structured form for LLM consumption.
//
// See ARCHITECTURE.md for the design.
package main

import (
	"fmt"
	"os"
)

// Build-time variables, set via -ldflags by the Makefile and goreleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "distill-ai:", err)
		os.Exit(2)
	}
}

func run(args []string) error {
	for _, a := range args {
		switch a {
		case "-v", "--version":
			fmt.Printf("distill-ai %s (commit %s, built %s)\n", version, commit, date)
			return nil
		case "-h", "--help":
			printHelp()
			return nil
		default:
			return fmt.Errorf("unknown flag %q (this is a placeholder build; see --help)", a)
		}
	}
	printHelp()
	return nil
}

func printHelp() {
	fmt.Println(`distill-ai — compress logs and test output for LLM consumption

Usage:
  cmd | distill-ai [FORMAT] [OPTIONS]
  distill-ai [FORMAT] [OPTIONS] FILE...

This is a placeholder build. The full CLI is under development.
See https://github.com/vail130/distill-ai for status.`)
}
