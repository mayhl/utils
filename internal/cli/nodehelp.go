package cli

import (
	"github.com/spf13/cobra"
)

// nodeHelpCmd is `mu setup node-help <node>` (hidden): what `<node> -h` prints.
//
// The per-node dispatcher is generated SHELL, so its help used to be a printf — flush-left,
// uncolored, and the one help text in mu that didn't look like mu. It doesn't have to live
// there: the shell can just call back into mu, and the grammar renders through the same
// house panels as every other command. Hidden because nobody types it — `<node> -h` does.
func nodeHelpCmd() *cobra.Command {
	c := &cobra.Command{
		Use:    "node-help <node>",
		Short:  "Render a node dispatcher's grammar (what `<node> -h` prints).",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			houseHelp(nodeGrammar(args[0]), nil)
			return nil
		},
	}
	return c
}

// nodeGrammar builds the synthetic command whose help IS the dispatcher's grammar. It has no
// parent, so its command path renders as the bare node name — which is exactly how the user
// types it.
func nodeGrammar(node string) *cobra.Command {
	c := &cobra.Command{
		Use:   node + " [verb]",
		Short: "HPC shorthand for the '" + node + "' cluster.",
		Long: "Shell shorthand for one cluster: bare `" + node + "` connects, a verb below runs the\n" +
			"mu command that takes a --node anyway, and anything else runs on " + node + " over ssh.\n" +
			"Generated from config.toml — a node you add there gets its own command.",
		Example: "    " + node + "                    # ssh to the default login node\n" +
			"    " + node + " 3                  # ssh to login node " + node + "03\n" +
			"    " + node + " shell --debug      # interactive allocation on a compute node\n" +
			"    " + node + " sub run.pbs -n 4   # submit a batch script\n" +
			"    " + node + " uptime             # run a command over ssh\n" +
			"    " + node + " exec queues        # run a PROGRAM called `queues`, not the verb",
	}
	setHelpTitle(c, node+" — HPC shorthand")
	setHelpArgs(
		c,
		[2]string{"N", "a login-node number: `" + node + " 3` targets " + node + "03 (zero-padded)"},
		[2]string{"<cmd>", "any other word: the command runs on " + node + " over ssh"},
	)
	// The verbs, as subcommands — so they land in the house Commands panel, and the reserved
	// words are visible as reserved (which is the thing the printf never made clear).
	for _, v := range [][2]string{
		{"shell", "interactive allocation on a compute node (mu job shell)"},
		{"sub", "submit a batch script (mu job sub)"},
		{"tunnel", "submit a service and tunnel its port (mu job tunnel)"},
		{"mstat", "show this cluster's queue (mu hpc queue)"},
		{"queues", "the cluster's queue list (mu hpc queues)"},
		{"usage", "allocation usage by subproject (mu hpc usage)"},
		{"storage", "disk quotas by filesystem (mu hpc storage)"},
		{"push", "upload: `push SRC [DST]`, DST defaults to $HOME (mu cp push)"},
		{"pull", "download: `pull SRC [DST]`, DST defaults to . (mu cp pull)"},
		{"exec", "run a command over ssh even if it is a verb above (`--` also works)"},
	} {
		// The empty Run is load-bearing: cobra calls a command with no Run "not available"
		// and the help panels skip it — these exist only to BE listed, never to run (the
		// shell dispatcher is what actually routes them).
		c.AddCommand(&cobra.Command{Use: v[0], Short: v[1], Run: func(*cobra.Command, []string) {}})
	}
	return c
}
