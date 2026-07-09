package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/job"
	"github.com/mayhl/mayhl_utils/internal/queue"
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
	c.AddCommand(jobEnvCmd(), jobSubCmd())
	return c
}

// jobSubCmd is `mu job sub <script>`: submit a batch script to one cluster (qsub/sbatch)
// with a scheduler-neutral account/queue, after a preview + confirm. Thin by design — the
// script path is resolved ON the target; -N picks the cluster off an HPC login node, else
// the current cluster. -A overrides the cluster's config default; empty opts fall through
// to the script's own #PBS/#SBATCH directives.
func jobSubCmd() *cobra.Command {
	var node, account, queue_ string
	var yes, dryRun bool
	c := &cobra.Command{
		Use:   "sub <script>",
		Short: "Submit a batch script to one cluster (qsub/sbatch) — preview + confirm.",
		Long: "Submit a job script to one cluster, mapping -A/-q to the scheduler's flags\n" +
			"(PBS qsub / SLURM sbatch). Target: -N <cluster> off an HPC login node, else the\n" +
			"current cluster. The script path is resolved ON the target. -A overrides the\n" +
			"cluster's config default; empty falls through to the script's own directives.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			script := args[0]
			label, scheduler, _, run, _ := queueTargetCtx(node, userSel{})
			adapter := queue.For(scheduler)
			if adapter == nil {
				errNoScheduler(label)
			}
			if account == "" {
				account = config.AccountFor(label)
			}
			opts := queue.SubmitOpts{Account: account, Queue: queue_}
			cmd := adapter.SubmitCmd(script, opts)

			render.Info(fmt.Sprintf("Submit to %s (%s)", label, scheduler))
			render.Detail("script:  " + script)
			if d := adapter.Directives(opts); len(d) > 0 {
				render.Detail("applies: " + strings.Join(d, "  "))
			} else {
				render.Detail("applies: (scheduler defaults / script directives)")
			}
			render.Detail("command: " + cmd)
			if dryRun {
				render.Info("dry run — not submitted")
				return nil
			}
			if !yes {
				fmt.Fprintf(os.Stderr, "submit to %s? [y/N] ", label)
				var r string
				_, _ = fmt.Scanln(&r)
				if strings.ToLower(strings.TrimSpace(r)) != "y" {
					render.Info("aborted")
					return nil
				}
			}
			if err := run(cmd); err != nil {
				return err
			}
			msg := "submitted " + script + " → " + label
			render.OK(msg)
			render.EventOK("job", msg)
			return nil
		},
	}
	c.Flags().StringVarP(&node, "node", "N", "", "cluster to target (required off an HPC login node)")
	c.Flags().StringVarP(&account, "account", "A", "", "allocation to charge (overrides the cluster's config default)")
	c.Flags().StringVarP(&queue_, "queue", "q", "", "queue / partition to submit to")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the submit command without submitting")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
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
