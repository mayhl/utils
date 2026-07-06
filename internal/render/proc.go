package render

import (
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// ProcRow is one process for ProcTable — plain fields only, so render stays
// domain-free (like MountRow / JobRow).
type ProcRow struct {
	PID, User, State, Elapsed, Command string
}

// ProcTable renders processes as the house table: PID / User / State / Elapsed /
// Command. Used by `mu ps` (view) and by `mu ps kill` to preview the kill set — the
// caller puts the count (and, for a kill, the signal) in the title. Command is
// cropped to the leftover terminal width so every row stays one line.
func ProcTable(title string, rows []ProcRow) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	t.SetTitle(title)
	t.AppendHeader(table.Row{"PID", "User", "State", "Elapsed", "Command"})
	for _, r := range rows {
		t.AppendRow(table.Row{r.PID, r.User, r.State, r.Elapsed, r.Command})
	}
	t.SetColumnConfigs(procCols(rows))
	t.Render()
}

// procCols caps User (long macOS daemon names like _spotlight/_windowserver) and
// crops Command to the leftover terminal width, both head-kept behind a trailing …,
// so a wide command never wraps the row. Mirrors the mounts/queue fit approach.
func procCols(rows []ProcRow) []table.ColumnConfig {
	const userCap = 16
	pidW, userW, stateW, elapW := len("PID"), len("User"), len("State"), len("Elapsed")
	for _, r := range rows {
		pidW = max(pidW, text.StringWidth(r.PID))
		userW = max(userW, text.StringWidth(r.User))
		stateW = max(stateW, text.StringWidth(r.State))
		elapW = max(elapW, text.StringWidth(r.Elapsed))
	}
	userW = min(userW, userCap)
	// StyleRounded overhead per column: 2 padding + a border glyph → 3*ncols + 1.
	overhead := 3*5 + 1
	budget := termWidth() - (pidW + userW + stateW + elapW + overhead)
	if budget < 8 {
		budget = 8 // floor: a narrow terminal still shows a stub, not nothing
	}
	return []table.ColumnConfig{
		{Name: "PID", Colors: tc(HueID)},
		{Name: "User", Colors: tc(HueUser), WidthMax: userCap, WidthMaxEnforcer: truncRight},
		{Name: "Command", WidthMax: budget, WidthMaxEnforcer: truncRight},
	}
}
