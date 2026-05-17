// Command distill-ai distills noisy command output (test runs,
// application logs, stack traces) into a compact, structured form for
// LLM consumption.
//
// See ARCHITECTURE.md for the design and the README for usage.
package main

import (
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
