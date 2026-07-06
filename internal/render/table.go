package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// applyStyle sets the house rounded style, or a borderless tab-aligned style when
// plainMode() (MU_RENDER=plain / --plain / piped). Plain implies no color — the
// status glyph (or MU_ASCII label) still carries meaning.
func applyStyle(t table.Writer) {
	if plainMode() {
		s := table.StyleDefault
		s.Options.DrawBorder = false
		s.Options.SeparateColumns = false
		s.Options.SeparateHeader = false
		t.SetStyle(s)
		text.DisableColors()
		return
	}
	t.SetStyle(table.StyleRounded)
	if colorOff() {
		text.DisableColors()
	}
}

// NodesTable renders `mu hpc nodes`: a framed username line plus a
// Cluster/Node/Host table (magenta cluster, bold-green node, cyan host, one
// cluster label per group, dividers between clusters). When status is non-empty
// (from `-s`), a reachability column is added — ● up (green) / ○ down (red),
// keyed by node name.
func NodesTable(defs []config.Cluster, user string, status map[string]string) {
	withStatus := len(status) > 0

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	// Username lives inside the title (like the sshfs list's LOCAL PATH line),
	// rather than in a separate box above the table.
	t.SetTitle("HPC Nodes\nUsername: " + user)
	header := table.Row{"Cluster", "Node", "Host"}
	if withStatus {
		header = append(header, "Status")
	}
	t.AppendHeader(header)
	for i, cl := range defs {
		for j, node := range cl.Nodes {
			label := ""
			if j == 0 {
				label = cl.Name
			}
			row := table.Row{label, node, node + "." + cl.Domain}
			if withStatus {
				row = append(row, nodeStatusBadge(status[node]))
			}
			t.AppendRow(row)
		}
		if i < len(defs)-1 {
			t.AppendSeparator()
		}
	}
	cols := []table.ColumnConfig{
		{Name: "Cluster", Colors: text.Colors{text.FgMagenta}},
		{Name: "Node", Colors: text.Colors{text.FgGreen, text.Bold}},
		{Name: "Host", Colors: text.Colors{text.FgCyan}},
	}
	if withStatus {
		cols = append(cols, table.ColumnConfig{Name: "Status", Transformer: nodeStatusTransformer})
	}
	t.SetColumnConfigs(cols)
	t.Render()
}

// StatusRow is one row of a StatusTable: a level ("ok"|"warn"|"error"|"info"), a
// name, and a detail string. Domain-free — any grouped check output can use it.
type StatusRow struct {
	Level, Name, Detail string
}

// StatusTable renders a titled rounded table of check rows, the status column a
// glyph colored by level. Used by `mu doctor` (one table per section).
func StatusTable(title string, rows []StatusRow) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	// House accents: cyan-bold title, cyan headers, dim frame, bold-magenta check
	// name, white detail — the colored status glyph still carries the verdict.
	t.Style().Title.Colors = text.Colors{text.FgCyan, text.Bold}
	t.Style().Color.Header = text.Colors{text.FgCyan}    // headers: cyan accent
	t.Style().Color.Border = text.Colors{text.FgHiBlack} // frame: quiet dim chrome
	t.Style().Color.Separator = text.Colors{text.FgHiBlack}
	t.SetTitle(title)
	t.AppendHeader(table.Row{"", "Check", "Detail"})
	for _, r := range rows {
		t.AppendRow(table.Row{statusCell(r.Level), r.Name, r.Detail})
	}
	// Detail left uncolored → terminal default foreground (white on dark), theme-aware.
	t.SetColumnConfigs([]table.ColumnConfig{
		{Name: "Check", Colors: text.Colors{text.FgMagenta, text.Bold}},
	})
	t.Render()
}

func statusCell(level string) string {
	switch strings.ToUpper(level) {
	case "OK":
		return text.Colors{text.FgGreen, text.Bold}.Sprint(glyph("✓", "OK"))
	case "WARN", "WARNING":
		return text.Colors{text.FgYellow, text.Bold}.Sprint(glyph("!", "WARN"))
	case "ERROR", "ERR", "FAIL":
		return text.Colors{text.FgRed, text.Bold}.Sprint(glyph("✗", "ERR"))
	default:
		return text.Colors{text.FgCyan}.Sprint(glyph("→", "INFO"))
	}
}

func nodeStatusBadge(status string) string {
	switch status {
	case "up":
		return glyph("●", "*") + " up"
	case "down":
		return glyph("○", "o") + " down"
	default:
		return glyph("?", "?") + " ?"
	}
}

func nodeStatusTransformer(v interface{}) string {
	s := fmt.Sprint(v)
	switch {
	case strings.Contains(s, "down"):
		return text.Colors{text.FgRed}.Sprint(s)
	case strings.Contains(s, "up"):
		return text.Colors{text.FgGreen}.Sprint(s)
	default:
		return text.Colors{text.FgHiBlack}.Sprint(s)
	}
}
