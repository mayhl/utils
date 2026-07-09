package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// queueInfoCmd is `mu hpc queue info` (front-door `minfo`): show full job detail as the
// house card — normalized from `qstat -f` (PBS) / `scontrol show job` (SLURM). Pretty by
// default (mu = one house format); --raw prints the scheduler's own output verbatim and
// --json emits the normalized detail (the data contract), the standard three-mode set.
// Single-cluster like mdel; selectors resolve short ids against your queue (-u/-a widen).
// No idiom variant — there's no clean squeue word for it, so `minfo` whatever queue_aliases.
func queueInfoCmd() *cobra.Command {
	var node, userList string
	var allUsers, pattern, raw, jsonOut bool
	c := &cobra.Command{
		Use:   "info <selector>...",
		Short: "Show job detail as a house card (qstat -f / scontrol show job).",
		Long: "Resolve selectors against one cluster's queue and show each match's full\n" +
			"detail as the house card, normalized across schedulers. --raw prints the\n" +
			"scheduler's own `qstat -f` / `scontrol show job` verbatim; --json emits the\n" +
			"normalized detail (data contract). Single-cluster: --node <cluster> off an HPC\n" +
			"login node, else the current one. Scoped to your jobs (-u/-a widen). A selector\n" +
			"is a job id (short or full), a range (4501-4510), a list (4501,4507), or a name\n" +
			"mask; -p forces a mask. Front-door: `minfo`.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			who := mustUserSel(userList, allUsers)
			label, scheduler, snapshot, _, capture := queueTargetCtx(node, who)
			matched := resolveJobs(label, snapshot, args, pattern)
			cmd := detailCmd(scheduler, jobIDs(matched))
			if cmd == "" {
				errNoScheduler(label)
			}
			out, err := capture(cmd)
			if err != nil {
				render.Err(fmt.Sprintf("%s: info fetch failed: %v", label, err))
				os.Exit(1)
			}
			if raw {
				fmt.Print(out)
				return nil
			}
			details := queue.ParseDetails(scheduler, out)
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(details)
			}
			for _, d := range details {
				render.JobDetailCard(toDetailView(d))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&raw, "raw", false, "print the scheduler's own detail output verbatim")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit the normalized detail as JSON")
	addQueueScopeFlags(c, &node, &userList, &allUsers, &pattern)
	return c
}

// toDetailView maps a normalized queue.JobDetail to render's plain card view (keeping
// render domain-free, like toJobRows).
func toDetailView(d queue.JobDetail) render.JobDetailView {
	return render.JobDetailView{
		ID: d.ID, Name: d.Name, User: d.User, Account: d.Account, Queue: d.Queue,
		State: d.State, RawState: d.RawState, Nodes: d.Nodes, Tasks: d.Tasks,
		Elapsed: d.Elapsed, ReqWall: d.ReqWall, Submit: d.Submit, Start: d.Start, End: d.End,
		WorkDir: d.WorkDir, StdOut: d.StdOut, StdErr: d.StdErr, ExitStatus: d.ExitStatus,
		Reason: d.Reason,
	}
}

// queuePeekCmd is `mu hpc queue peek` (front-door `mpeek`): tail a job's output file.
// It reads the job's Output_Path/StdOut from the scheduler detail, then tails that file
// — not `qpeek`. Live for SLURM (StdOut is written to the shared FS as the job runs);
// for a running PBS job whose output still sits in node-local spool, the file may not
// be readable until it completes (a hint is shown on failure). One job at a time.
func queuePeekCmd() *cobra.Command {
	var node, userList string
	var allUsers, stderr bool
	var lines int
	c := &cobra.Command{
		Use:   "peek <selector>",
		Short: "Tail a job's stdout (or -e stderr) output file.",
		Long: "Resolve a selector to one job, read its output-file path from the scheduler\n" +
			"detail (Output_Path / StdOut), and tail it — the last -n lines of stdout, or\n" +
			"stderr with -e. Not `qpeek`: it reads the real file, so for a running PBS job\n" +
			"whose output is still in node-local spool it may only work once the job ends.\n" +
			"Single-cluster (--node off HPC). Front-door: `mpeek`.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			who := mustUserSel(userList, allUsers)
			label, scheduler, snapshot, _, capture := queueTargetCtx(node, who)
			matched := resolveJobs(label, snapshot, args, false)
			job := matched[0]
			if len(matched) > 1 {
				render.Info(fmt.Sprintf("%d jobs matched — peeking %s (%s); narrow the selector for another", len(matched), job.ShortID, job.Name))
			}
			detail, err := capture(detailCmd(scheduler, []string{job.ID}))
			if err != nil {
				render.Err(fmt.Sprintf("%s: detail fetch failed: %v", label, err))
				os.Exit(1)
			}
			stream := "stdout"
			if stderr {
				stream = "stderr"
			}
			path := queue.OutputPath(scheduler, detail, stderr)
			if path == "" {
				render.Warn(fmt.Sprintf("no %s path reported for %s — the job may not have started yet", stream, job.ShortID))
				os.Exit(1)
			}
			out, err := capture(fmt.Sprintf("tail -n %d %s", lines, shellQuote(path)))
			if err != nil {
				render.Err(fmt.Sprintf("%s: cannot read %s (%v) — a running PBS job's output may still be in node-local spool", label, path, err))
				os.Exit(1)
			}
			render.Info(fmt.Sprintf("%s  %s  %s (last %d)", job.ShortID, stream, path, lines))
			fmt.Print(out)
			return nil
		},
	}
	c.Flags().BoolVarP(&stderr, "stderr", "e", false, "tail the job's stderr file instead of stdout")
	c.Flags().IntVarP(&lines, "lines", "n", 40, "number of trailing lines to show")
	addQueueScopeFlags(c, &node, &userList, &allUsers, nil)
	return c
}

// addQueueScopeFlags registers the WHERE/WHO/selector flags the queue verbs share:
// --node target, -a/-u user scope, and (when patternFlag is non-nil) -p force-mask.
func addQueueScopeFlags(c *cobra.Command, node, userList *string, allUsers, patternFlag *bool) {
	c.Flags().StringVarP(node, "node", "N", "", "cluster to target (required off an HPC login node)")
	c.Flags().BoolVarP(allUsers, "all-users", "a", false, "widen to all users' jobs (default: yours)")
	c.Flags().StringVarP(userList, "user", "u", "", "widen to these users' jobs (comma-separated)")
	if patternFlag != nil {
		c.Flags().BoolVarP(patternFlag, "pattern", "p", false, "force every argument to be a name mask")
	}
	c.MarkFlagsMutuallyExclusive("all-users", "user")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
}

// mustUserSel builds the WHO axis from -u/-a, exiting with a house error on a malformed
// user list (it's interpolated into the fetch command). Shared by the queue verbs.
func mustUserSel(userList string, allUsers bool) userSel {
	if userList != "" && !validUserList(userList) {
		render.Err("--user takes a comma-separated user list (letters/digits/._-), e.g. -u alice,bob")
		os.Exit(2)
	}
	return userSel{all: allUsers, list: userList}
}

// resolveJobs snapshots the target queue and resolves selector args to matched jobs
// (short-id → full, ranges, lists, name masks). It exits — cleanly on an empty match,
// with a house error on a fetch failure — so callers get a non-empty set or nothing.
func resolveJobs(label string, snapshot func() ([]queue.Job, error), args []string, pattern bool) []queue.Job {
	jobs, err := snapshot()
	if err != nil {
		render.Err(fmt.Sprintf("%s: queue fetch failed: %v", label, err))
		os.Exit(1)
	}
	matched := queue.MatchAll(jobs, args, pattern)
	if len(matched) == 0 {
		render.Info("no matching jobs on " + label)
		os.Exit(0)
	}
	return matched
}

// detailCmd builds the scheduler's full-detail command for the given full ids. PBS
// `qstat -f` takes space-separated ids; SLURM `scontrol show job` takes a comma list.
// Ids are single-quoted so PBS array brackets ("1284[7].hpc1") don't glob-expand. ""
// for an unknown scheduler.
func detailCmd(scheduler string, ids []string) string {
	if a := queue.For(scheduler); a != nil {
		return a.DetailCmd(ids)
	}
	return ""
}

// shellQuote wraps s in single quotes, escaping any embedded single quote.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
