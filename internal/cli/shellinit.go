package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/shellinit"
)

func shellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init",
		Short: "Emit shell integration to eval at startup.",
		Long: "Print the shell integration generated from config.toml — a per-node\n" +
			"dispatcher for every configured node. Add to your shell rc:\n\n" +
			"    eval \"$(mu shell-init)\"\n\n" +
			"Then, per node (e.g. mike):\n" +
			"    mike              connect (ssh login)\n" +
			"    mike push <l> <r> copy local → mike\n" +
			"    mike pull <r> <l> copy mike → local\n" +
			"    mike <cmd>        run <cmd> on mike over ssh",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Print(shellinit.Generate())
			return nil
		},
	}
}
