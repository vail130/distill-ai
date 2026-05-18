package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCompletionsCmd returns the cobra command for
// `distill-ai completions [bash|zsh|fish|powershell]`. It writes a
// shell completion script to stdout suitable for sourcing.
//
// We choose 'completions' (plural) over cobra's default 'completion'
// (singular) for parity with what the CLI promises in
// ARCHITECTURE.md and the dedicated subcommand spec in TODO M8.7.
//
// Exit codes:
//
//	0  Script written successfully.
//	2  Unknown shell argument or missing shell argument.
func newCompletionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completions [bash|zsh|fish|powershell]",
		Short: "Generate a shell completion script.",
		Long: `completions writes a shell completion script for the
named shell to stdout. Pipe the output into a shell-specific
source location to enable tab completion.

Bash (one-shot for the current session):
   source <(distill-ai completions bash)

Bash (system-wide install):
   distill-ai completions bash > /etc/bash_completion.d/distill-ai

Zsh:
   distill-ai completions zsh > "${fpath[1]}/_distill-ai"

Fish:
   distill-ai completions fish > ~/.config/fish/completions/distill-ai.fish

PowerShell:
   distill-ai completions powershell > distill-ai.ps1`,
		Args:                  cobra.ExactArgs(1),
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		DisableFlagsInUseLine: true,
		RunE:                  runCompletions,
	}
	return cmd
}

// runCompletions dispatches on the shell argument and writes the
// matching cobra-generated completion script to stdout. The shell
// list is held in sync with newCompletionsCmd's ValidArgs.
func runCompletions(cmd *cobra.Command, args []string) error {
	root := cmd.Root()
	w := cmd.OutOrStdout()
	switch args[0] {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(w)
	default:
		// cobra's ValidArgs already filters this, but defence in
		// depth: a future flag refactor that loosens Args might
		// otherwise drop into a confusing no-op.
		fmt.Fprintf(cmd.ErrOrStderr(), "distill-ai completions: unknown shell %q (want bash | zsh | fish | powershell)\n", args[0])
		return &exitCodeError{code: ExitError}
	}
}
