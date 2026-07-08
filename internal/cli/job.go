package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/job"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// jobCmd is `mu job`: helpers for HPC batch jobs that work across PBS and SLURM.
func jobCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "job",
		Short: "HPC batch-job helpers (scheduler-agnostic).",
		Long:  "Helpers for HPC batch jobs that behave the same under PBS and SLURM.",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	c.AddCommand(jobEnvCmd())
	return c
}

// jobEnvCmd is `mu job env`: the runtime shim — normalize the active scheduler's in-job
// vars to MU_* for `eval` at the top of a job script.
func jobEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Emit normalized MU_* job vars for `eval` inside a job script.",
		Long: "Print `export MU_*` lines that normalize the active scheduler's in-job\n" +
			"variables (PBS or SLURM) to a common MU_* set, so one job script runs under\n" +
			"either. Source it at the top of a job script:\n\n" +
			"    eval \"$(mu job env)\"\n\n" +
			"then use $MU_JOBID, $MU_SUBMIT_DIR, $MU_NUM_NODES, $MU_NODEFILE, …. On SLURM,\n" +
			"$MU_NODEFILE is a hostname-per-line file expanded from the compressed nodelist,\n" +
			"so it matches PBS's $PBS_NODEFILE.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			out, err := job.Env()
			if err != nil {
				render.Err(err.Error())
				os.Exit(1)
			}
			fmt.Print(out)
			return nil
		},
	}
}
