package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// NodesTable renders `mu hpc nodes`: a framed username line plus a
// Cluster/Node/Host table (magenta cluster, bold-green node, cyan host, one
// cluster label per group, dividers between clusters). When status is non-empty
// (from `-s`), a reachability column is added — ● up (green) / ○ down (red),
// keyed by node name.
func NodesTable(defs []config.Cluster, user string, status map[string]string) {
	if colorOff() {
		text.DisableColors()
	}
	withStatus := len(status) > 0

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
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
