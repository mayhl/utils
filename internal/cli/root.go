// Package cli assembles the Cobra command tree for `mu`. Charm's fang wraps the
// root at Execute time to style help/errors in the house visual language.
package cli

import "github.com/spf13/cobra"

// Root builds the top-level `mu` command with all subcommand trees attached.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "mu",
		Short: "mayhl_utils — HPC toolkit",
		// fang/main own error + usage printing; RunE returns bare errors for
		// exit codes without Cobra also dumping usage on a runtime failure.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	return root
}
