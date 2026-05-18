package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd returns the cobra command for `distill-ai version`.
// It prints the build info one field per line. The top-level
// --version flag already shows the same info as a single-line
// "distill-ai <ver> (commit X, built Y)"; the subcommand exists for
// CLI consistency (some tooling expects `tool version` to work) and
// for scripts that want the fields on separate lines so cut / awk
// can extract one without parsing prose.
//
// Exit code: always 0.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version, commit, and build date.",
		Long: `version prints the build info one field per line:

  version: <semver-or-dev>
  commit:  <git-sha>
  date:    <build-time>

The values come from ldflags injected at build time by the
Makefile (and by goreleaser for tagged releases). In a 'go run'
or unstripped 'go build' invocation they default to dev / none /
unknown.

The top-level --version flag prints the same information as a
single human-readable line.`,
		Args: cobra.NoArgs,
		Run:  runVersion,
	}
}

// runVersion writes the three fields to stdout. Uses Run rather
// than RunE because the command cannot fail.
func runVersion(cmd *cobra.Command, _ []string) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "version: %s\n", version)
	fmt.Fprintf(w, "commit:  %s\n", commit)
	fmt.Fprintf(w, "date:    %s\n", date)
}
