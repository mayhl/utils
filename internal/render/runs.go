package render

import (
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// RunRow is one provenance record for RunsTable — a plain mirror of
// project.Run (render stays domain-free, like JobRow).
type RunRow struct {
	JobID, Case, Cluster, Queue, Started, Commit string
	Dirty                                        bool
}

// RunsTable renders the run-provenance listing: Job / Case / Cluster / Queue /
// Started / Commit. The commit cell carries the dirty verdict — a clean sha in
// default fg, "+dirty" appended in yellow (status color on the status fact) —
// and timestamps expand through the card's verbose form.
func RunsTable(title string, rows []RunRow) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	t.SetTitle(title)
	t.AppendHeader(table.Row{"Job", "Case", "Cluster", "Queue", "Started", "Commit"})
	for _, r := range rows {
		t.AppendRow(table.Row{
			r.JobID, dash(r.Case), dash(r.Cluster), dash(r.Queue),
			dash(longTime(r.Started)), commitCell(r.Commit, r.Dirty),
		})
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Name: "Job", Colors: append(tc(HueID), text.Bold)},
		{Name: "Cluster", Colors: tc(HueLoc)},
		{Name: "Queue", Colors: tc(HueGroup)},
		{Name: "Started", Colors: tc(HueDim)},
	})
	t.Render()
}

// commitCell is the short sha plus the dirty verdict ("-" when no git record).
func commitCell(commit string, dirty bool) string {
	if strings.TrimSpace(commit) == "" {
		return "-"
	}
	cell := commit
	if len(cell) > 12 {
		cell = cell[:12]
	}
	if dirty {
		cell += " " + text.Colors{text.FgYellow}.Sprint("+dirty")
	}
	return cell
}
