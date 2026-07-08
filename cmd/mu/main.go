// Command mu is the mayhl_utils HPC toolkit CLI (Go engine).
//
// The interactive shell layer (init.sh, connect codegen, ssh/auth aliases)
// stays in shell; this binary is the operation engine that the `mu` launcher
// execs. Config is read from the inherited (exported) shell env — same contract
// as the retired Python CLI (MU_CLUSTERS, MU_CLUSTER_<X>_{DOMAIN,NODES}, …).
package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"

	"github.com/mayhl/mayhl_utils/internal/cli"
)

func main() {
	root := cli.Root()
	// fang owns the root's help func with no override (it hardcodes root.SetHelpFunc), so
	// intercept bare-root help here to render it in the house language. Subcommand help
	// still flows through fang→cobra to each command's wrapHelp; fang keeps version chrome.
	if cli.MaybeRootHelp(root, os.Args[1:]) {
		return
	}
	if err := fang.Execute(context.Background(), root,
		fang.WithColorSchemeFunc(cli.HelpColorScheme),
		fang.WithErrorHandler(cli.HouseError)); err != nil {
		os.Exit(1)
	}
}
