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
	c.AddCommand(jobEnvCmd(), jobSubCmd(), jobPrepCmd(), jobHooksCmd(), jobTunnelCmd(), jobShellCmd())
	return c
}

// jobPrepCmd is `mu job prep`: create this job's run dir (sibling case_a_<jobid>,
// inputs copied, run.toml provenance) and emit the shell that moves the job into it.
// stdout is CODE for eval — mu owns the failure semantics (a failed prep emits
// `exit 1`, so the job dies instead of computing in the authored case dir); all
// human-facing lines go to stderr.
func jobPrepCmd() *cobra.Command {
	var pathOnly bool
	c := &cobra.Command{
		Use:   "prep",
		Short: "Create the run dir (case copy + run.toml) — eval inside a job.",
		Long: "Give this job its own run dir: copy the submit (case) dir to a sibling named\n" +
			"<case>_<jobid> (scheduler log files excluded), drop a run.toml provenance record,\n" +
			"and print shell that exports MU_RUN_DIR and cds there — a failed prep prints\n" +
			"`exit 1` so the job aborts rather than running in the case dir. Preamble idiom:\n\n" +
			"    eval \"$(mu job env)\"\n" +
			"    eval \"$(mu job prep)\"\n\n" +
			"A requeue (run dir already present) reuses it without re-copying, so partial\n" +
			"outputs survive. --path prints the run dir path and changes nothing.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if pathOnly {
				dir, err := job.RunDir()
				if err != nil {
					return runErr("%s", err)
				}
				fmt.Println(dir)
				return nil
			}
			snippet, reused, err := job.Prep()
			if err != nil {
				// stdout carries the abort for the eval'ing job script; the returned
				// error covers interactive use (stderr + non-zero exit).
				fmt.Printf("echo 'mu job prep: %s' >&2\nexit 1\n", strings.ReplaceAll(err.Error(), "'", `'\''`))
				return runErr("%s", err)
			}
			if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(os.Getenv("MU_SUBMIT_DIR")+"/", home+"/") {
				render.Warn("submit dir is under $HOME — this run will write to the permanent tier (submit from the $WORKDIR copy)")
			}
			fmt.Print(snippet)
			verb := "prepared"
			if reused {
				verb = "reusing"
			}
			dir, _ := job.RunDir()
			render.OK(verb + " run dir " + dir)
			render.EventOK("job", verb+" run dir "+dir)
			return nil
		},
	}
	c.Flags().BoolVar(&pathOnly, "path", false, "print the run dir path only (no copy, no run.toml)")
	return c
}

// jobSubCmd is `mu job sub <script>`: submit a batch script to one cluster (qsub/sbatch)
// with a scheduler-neutral account/queue, after a preview + confirm. Thin by design — the
// script path is resolved ON the target; -N picks the cluster off an HPC login node, else
// the current cluster. -A overrides the cluster's config default; empty opts fall through
// to the script's own #PBS/#SBATCH directives.
func jobSubCmd() *cobra.Command {
	var node, account, queue_ string
	var gpu, vis, bigmem, himem, xfer, debug, dbg, background, back bool
	var yes, dryRun bool
	c := &cobra.Command{
		Use:   "sub <script>",
		Short: "Submit a batch script to one cluster (qsub/sbatch) — preview + confirm.",
		Long: "Submit a job script to one cluster, mapping -A/-q to the scheduler's flags\n" +
			"(PBS qsub / SLURM sbatch). Target: -N <cluster> off an HPC login node, else the\n" +
			"current cluster. The script path is resolved ON the target. -A overrides the\n" +
			"cluster's config default; empty falls through to the script's own directives.\n\n" +
			"The class flags pick the queue by node class instead of by name: --gpu/--vis/\n" +
			"--bigmem/--xfer use the cluster's config `submit_queue` entry, else resolve it\n" +
			"from the live queue list (exactly one queue of that class); --debug/--background\n" +
			"fall back to the standard queue of that name. With no -q and no class flag,\n" +
			"`submit_queue.default` applies when set.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			script := args[0]
			label, scheduler, _, run, _, err := queueTargetCtx(node, userSel{})
			if err != nil {
				return err
			}
			adapter := queue.For(scheduler)
			if adapter == nil {
				return errNoScheduler(label)
			}
			if account == "" {
				account = config.AccountFor(label)
			}
			if queue_ == "" {
				key := "" // "" → the bare-sub default
				switch {
				case gpu:
					key = "gpu"
				case vis:
					key = "vis"
				case bigmem || himem:
					key = "bigmem"
				case xfer:
					key = "xfer"
				case debug || dbg:
					key = "debug"
				case background || back:
					key = "background"
				}
				if queue_, err = resolveSubmitQueue(node, label, key); err != nil {
					return err
				}
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
	setHelpArgs(c, [2]string{"<script>", "job script path, resolved ON the target cluster"})
	c.Flags().StringVarP(&node, "node", "N", "", "cluster to target (required off an HPC login node)")
	c.Flags().StringVarP(&account, "account", "A", "", "allocation to charge (overrides the cluster's config default)")
	c.Flags().StringVarP(&queue_, "queue", "q", "", "queue / partition to submit to")
	c.Flags().BoolVar(&gpu, "gpu", false, "submit to the GPU queue (config submit_queue.gpu, else resolved live)")
	c.Flags().BoolVar(&vis, "vis", false, "submit to the visualization queue (config submit_queue.vis, else resolved live)")
	c.Flags().BoolVar(&bigmem, "bigmem", false, "submit to the big-memory queue (config submit_queue.bigmem, else resolved live)")
	c.Flags().BoolVar(&himem, "himem", false, "alias for --bigmem")
	c.Flags().BoolVar(&xfer, "xfer", false, "submit to the transfer/archive queue (config submit_queue.xfer, else resolved live)")
	c.Flags().BoolVar(&debug, "debug", false, "submit to the debug queue — quick iterations (config submit_queue.debug, else the queue named 'debug')")
	c.Flags().BoolVar(&dbg, "dbg", false, "alias for --debug")
	c.Flags().BoolVar(&background, "background", false, "submit to the no-charge background queue (config submit_queue.background, else the queue named 'background')")
	c.Flags().BoolVar(&back, "back", false, "alias for --background")
	_ = c.Flags().MarkHidden("himem")
	_ = c.Flags().MarkHidden("dbg")
	_ = c.Flags().MarkHidden("back")
	c.MarkFlagsMutuallyExclusive("queue", "gpu", "vis", "bigmem", "himem", "xfer", "debug", "dbg", "background", "back")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the submit command without submitting")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

// submitClasses maps a class-flag key to the node class the live fallback filters by.
// debug/background are absent: purpose tiers, not node classes — their queue names are
// standard across systems, so they fall back to the literal name (submitLiterals).
var (
	submitClasses  = map[string]string{"gpu": "GPU", "vis": "VIS", "bigmem": "BigMem", "xfer": "Xfer"}
	submitLiterals = map[string]string{"debug": "debug", "background": "background"}
)

// resolveSubmitQueue picks the submit queue for a selector-flag key (gpu/vis/bigmem/
// xfer/debug/background), or for the bare default (key ""). Order: the cluster's config
// submit_queue entry; then the standard literal name for the purpose keys; else the
// live queue list filtered by class — exactly one up submittable match resolves, zero
// or several error with a submit_queue hint. node routes the fetch (""=local cluster).
func resolveSubmitQueue(node, label, key string) (string, error) {
	if key == "" {
		return config.SubmitQueueFor(label, "default"), nil
	}
	if q := config.SubmitQueueFor(label, key); q != "" {
		return q, nil
	}
	if q, ok := submitLiterals[key]; ok {
		return q, nil
	}
	var (
		qs  []queue.QueueInfo
		err error
	)
	if node != "" {
		_, qs, err = fetchQueues(node)
	} else {
		_, qs, err = fetchQueuesLocal()
	}
	if err != nil {
		return "", err
	}
	class := submitClasses[key]
	names := classQueues(label, class, qs)
	switch len(names) {
	case 1:
		render.Info(fmt.Sprintf("--%s → queue %s (from the live queue list; pin it: submit_queue = { %s = %q })", key, names[0], key, names[0]))
		return names[0], nil
	case 0:
		return "", runErr("no up %s queue on %s — set submit_queue = { %s = \"<queue>\" } in config.toml, or use -q", class, label, key)
	default:
		return "", runErr("%d %s queues on %s (%s) — pick one with -q, or set submit_queue = { %s = \"<queue>\" }", len(names), class, label, strings.Join(names, ", "), key)
	}
}

// classQueues is the pure core of the live class-flag fallback: the up, submittable
// queues on label whose node class matches — the config queue_class override first,
// the name heuristic else. Input order preserved.
func classQueues(label, class string, qs []queue.QueueInfo) []string {
	up, _ := upQueues(execQueues(qs))
	var names []string
	for _, q := range up {
		if queueClass(label, q.Name) == class {
			names = append(names, q.Name)
		}
	}
	return names
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
				return runErr("%s", err)
			}
			fmt.Print(out)
			return nil
		},
	}
}
