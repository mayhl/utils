package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// queueHoldCmd / queueReleaseCmd are `mu hpc queue hold|release` (front-doors mhold /
// mrls): place or lift a hold on your jobs on ONE cluster (qhold/qrls, SLURM scontrol
// hold/release). Single-cluster and selector-resolved like mdel — but hold is
// reversible, so unlike a cancel it previews and event-logs without a confirm prompt.
func queueHoldCmd() *cobra.Command {
	return holdReleaseCmd("hold", "Hold", "held", false)
}

func queueReleaseCmd() *cobra.Command {
	return holdReleaseCmd("release", "Release", "released", true)
}

// holdReleaseCmd builds the shared hold/release command — use is the verb, title the
// preview heading, past the log/OK past-tense; release picks qrls vs qhold.
func holdReleaseCmd(use, title, past string, release bool) *cobra.Command {
	var node, userList string
	var allUsers, pattern bool
	c := &cobra.Command{
		Use:   use + " <selector>...",
		Short: title + " your jobs on one cluster (reversible — previewed, no confirm).",
		Long: "Resolve selectors against one cluster's queue and " + use + " the matches.\n" +
			"Single-cluster: --node <cluster> off an HPC login node, else the current one.\n" +
			"Scoped to your jobs (-u/-a widen). A selector is a job id (short or full), a\n" +
			"range (4501-4510), a list (4501,4507), or a name mask; -p forces a mask. Being\n" +
			"reversible, it previews and logs the set but does not prompt for confirmation.\n" +
			"Front-doors: `mhold` / `mrls`.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			who, err := mustUserSel(userList, allUsers)
			if err != nil {
				return err
			}
			label, scheduler, snapshot, run, _, err := queueTargetCtx(node, who)
			if err != nil {
				return err
			}
			matched, err := resolveJobs(label, snapshot, args, pattern)
			if err != nil {
				return err
			}
			if len(matched) == 0 {
				return nil
			}
			cmd := holdCmd(scheduler, release, jobIDs(matched))
			return actOnJobs(label, title, past, cmd, matched, run)
		},
	}
	setHelpArgs(c, [2]string{"<selector>", argJobSelectorDesc})
	addQueueScopeFlags(c, &node, &userList, &allUsers, &pattern)
	return c
}

// actOnJobs previews the matched set, runs a scheduler mutation (hold/release), and
// event-logs it. Reversible ops skip the confirm prompt cancel uses; the verb context
// lives in the preview title and the logged past-tense message.
func actOnJobs(label, title, past, cmd string, matched []queue.Job, run func(string) error) error {
	render.JobsTable(title+" on "+label, config.User(), toJobRows(matched), render.JobCols{})
	if cmd == "" {
		return errNoScheduler(label)
	}
	if err := run(cmd); err != nil {
		return err
	}
	msg := fmt.Sprintf("%s %d job(s) on %s", past, len(matched), label)
	render.OK(msg)
	render.EventOK("queue", msg)
	return nil
}

// holdCmd builds the scheduler's hold (or release) command for the given full ids —
// one batched qhold/qrls (PBS) or scontrol hold/release (SLURM). Ids are single-quoted
// so PBS array brackets don't glob. "" for an unknown scheduler.
func holdCmd(scheduler string, release bool, ids []string) string {
	if a := queue.For(scheduler); a != nil {
		return a.HoldCmd(ids, release)
	}
	return ""
}
