package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// queueHistCmd is `mu hpc queue hist` (front-door `mhist`): recent FINISHED jobs as the
// house table — SLURM `sacct`, PBS `qstat -x`. Single-cluster like the other queue
// verbs (--node off HPC). Scoped to your jobs (-u/-a widen). The window is each
// scheduler's default retention (sacct: since midnight; qstat -x: the server's finished
// history). No idiom variant — the name is `mhist` whatever queue_aliases is.
func queueHistCmd() *cobra.Command {
	var node, userList string
	var allUsers, times bool
	c := &cobra.Command{
		Use:   "hist",
		Short: "Show recent finished jobs as a house table (sacct / qstat -x).",
		Long: "Render recently finished jobs — SLURM `sacct`, PBS `qstat -x` — as the same\n" +
			"normalized table `mstat` uses, with an Ended column (when each job finished).\n" +
			"Single-cluster: --node <cluster> off an HPC login node, else the current one.\n" +
			"Scoped to your jobs (-u/-a widen). The window is the scheduler's own default\n" +
			"(sacct since midnight; qstat -x the server's finished history). --times adds\n" +
			"Submitted/Started columns for the full timeline. Submit/end are SLURM-only for\n" +
			"now — PBS `qstat -xa` doesn't carry them, so they show `--`. Front-door: `mhist`.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			who := mustUserSel(userList, allUsers)
			label, scheduler, _, _, capture := queueTargetCtx(node, who)
			cmd, parse := histSpec(scheduler, who)
			if cmd == "" {
				render.Err(fmt.Sprintf("no scheduler configured for %s — set `scheduler = \"slurm\"|\"pbs\"` in config.toml", label))
				os.Exit(2)
			}
			out, err := capture(cmd)
			if err != nil {
				render.Err(fmt.Sprintf("%s: history fetch failed: %v", label, err))
				os.Exit(1)
			}
			cols := render.JobCols{End: true}
			if times {
				cols = render.JobCols{Submit: true, Start: true, End: true}
			}
			render.JobsTable(label+" — history", config.User(), toJobRows(parse(out)), cols)
			return nil
		},
	}
	c.Flags().BoolVar(&times, "times", false, "add Submitted/Started columns for the full timeline")
	addQueueScopeFlags(c, &node, &userList, &allUsers, nil)
	return c
}

// histSpec returns the finished-job command + parser for a scheduler, applying the WHO
// axis. PBS reuses the wide `qstat -xa` (ParsePBS handles its columns); SLURM uses a
// controlled pipe-delimited `sacct` (ParseSacct). sacct has no --me, so "just you" names
// the configured user explicitly. "" cmd = unknown scheduler.
func histSpec(scheduler string, who userSel) (string, func(string) []queue.Job) {
	switch scheduler {
	case "pbs":
		sel := ""
		switch {
		case who.list != "":
			sel = " -u " + who.list
		case who.all:
			sel = ""
		default:
			if u := config.HPCUser(); u != "" {
				sel = " -u " + u
			}
		}
		return "qstat -xa" + sel, queue.ParsePBS
	case "slurm":
		sel := ""
		switch {
		case who.list != "":
			sel = "-u " + who.list + " "
		case who.all:
			sel = "-a "
		default:
			if u := config.HPCUser(); u != "" {
				sel = "-u " + u + " "
			}
		}
		return `sacct -X -n -p ` + sel + `-o JobIDRaw,JobName,User,Partition,State,Elapsed,Timelimit,NNodes,Submit,Start,End`, queue.ParseSacct
	default:
		return "", nil
	}
}
