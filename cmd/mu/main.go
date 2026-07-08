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
	if err := fang.Execute(context.Background(), cli.Root(),
		fang.WithColorSchemeFunc(cli.HelpColorScheme),
		fang.WithErrorHandler(cli.HouseError)); err != nil {
		os.Exit(1)
	}
}
