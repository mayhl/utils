package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/project"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// projectRunsCmd is `mu project runs`: tabulate the run.toml provenance records
// planted by `mu job prep` — the database rows without the database.
func projectRunsCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "runs [path]",
		Short: "Tabulate the project's run.toml provenance records.",
		Long: "Walk the project tree and its local $WORKDIR staging mirror for the run.toml\n" +
			"records `mu job prep` plants (jobid, cluster, queue, case, the inputs' commit,\n" +
			"dirty) and list them newest first. Records travel with the runs, so each\n" +
			"machine answers for the tiers it holds — run it on the cluster for live runs,\n" +
			"on the laptop for anything pulled back.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			root, err := project.FindRoot(path)
			if err != nil {
				return usageErr("%s", err)
			}
			runs := project.CollectRuns(project.RunTrees(root))
			if jsonOut {
				return writeJSON(runs)
			}
			if len(runs) == 0 {
				render.Info("no run.toml records under " + root + " (or its staging mirror)")
				return nil
			}
			rows := make([]render.RunRow, len(runs))
			for i, r := range runs {
				rows[i] = render.RunRow{
					JobID: r.JobID, Case: r.Case, Cluster: r.Cluster, Queue: r.Queue,
					Started: r.Started, Commit: r.Commit, Dirty: r.Dirty,
				}
			}
			render.RunsTable(fmt.Sprintf("Runs · %s (%d)", root, len(rows)), rows)
			return nil
		},
	}
	setHelpArgs(c, [2]string{"[path]", "a path inside the project (default: the current directory)"})
	c.Flags().BoolVar(&jsonOut, "json", false, "emit the records as JSON (complete, untruncated)")
	return c
}
