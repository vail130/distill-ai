package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vail130/distill-ai/internal/formats"
)

// newListFormatsCmd returns the cobra command for
// `distill-ai list-formats`. It prints one line per registered
// format: name, version, source. The output is deterministic
// (alphabetical by name) because formats.All() already sorts.
//
// Today every format is "builtin" with version "1". The version
// column exists so future format-author work (e.g., a plugin
// loader in some post-M16 milestone) can carry per-format
// versioning without breaking the line shape.
//
// Exit codes: 0 on success. The listing is informational; no
// error path returns a non-zero code.
func newListFormatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-formats",
		Short: "List every registered format with its version and source.",
		Long: `list-formats prints every format the binary has registered,
one per line, in alphabetical order by name. The columns are
name, version, source separated by tab characters so the output
is consumable both by humans and by shell scripts.

The list reflects only what is wired into the binary today. With
no specific formats yet shipped (M9 / M10 / M11 / M12), the only
visible entry will be 'generic' once M9 lands. Until then the
output is empty.`,
		Args: cobra.NoArgs,
		RunE: runListFormats,
	}
}

// runListFormats is the cobra RunE entry point. It walks
// formats.All() and writes a tab-separated triple per format.
//
//nolint:unparam // error return matches cobra RunE; no failure path today
func runListFormats(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()
	for _, f := range formats.All() {
		fmt.Fprintf(w, "%s\t1\tbuiltin\n", f.Name())
	}
	return nil
}
