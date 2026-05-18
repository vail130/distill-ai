// Command distill-ai distills noisy command output (test runs,
// application logs, stack traces) into a compact, structured form for
// LLM consumption.
//
// The cobra command tree lives in internal/cli so the man-page
// generator (cmd/distill-ai/gen-man) can build the same tree without
// duplicating wiring.
//
// See ARCHITECTURE.md for the design and the README for usage.
package main

import (
	"os"

	"github.com/vail130/distill-ai/internal/cli"
)

// Build-time variables, set via -ldflags by the Makefile and
// goreleaser. Forwarded to internal/cli via SetBuildInfo so the
// version subcommand and --version flag have access to them.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.SetBuildInfo(version, commit, date)
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
