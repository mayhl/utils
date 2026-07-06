package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// JobRow is one scheduler job for JobsTable — plain fields only, so render stays
// domain-free (like MountRow). State is a normalized label ("running", "queued",
// "held", "exiting", "complete", "waiting", "suspended") or a raw scheduler code for
// anything unrecognized; the table maps it to a glyph + color.
type JobRow struct {
	ID, Name, Queue, Nodes, State, Elapsed, ReqWall, Reason string
}

// JobsTable renders a scheduler queue as the house table: ID / Name / Queue / NDS /
// State / Elap·Wall. The state cell is a glyph colored by state (● green running / ○
// dim queued / ! yellow held / …); the title carries the cluster + a job-count
// summary. Long names truncate on the right to keep the table from wrapping.
func JobsTable(cluster, user string, rows []JobRow) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	t.SetTitle(jobsTitle(cluster, user, rows))
	t.AppendHeader(table.Row{"ID", "Name", "Queue", "NDS", "State", "Elap / Wall"})
	for _, r := range rows {
		// A pending reason (SLURM: why the job waits) rides in the state cell; a
		// running job carries none, so nothing is appended.
		state := jobStateBadge(r.State)
		if r.Reason != "" {
			state += " · " + r.Reason
		}
		t.AppendRow(table.Row{
			r.ID, r.Name, r.Queue, dash(r.Nodes),
			state, elapWall(r.Elapsed, r.ReqWall),
		})
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Name: "ID", Colors: text.Colors{text.FgGreen, text.Bold}},
		{Name: "Name", WidthMax: 28, WidthMaxEnforcer: truncRight},
		{Name: "Queue", Colors: text.Colors{text.FgMagenta}},
		{Name: "State", Transformer: jobStateTransformer},
	})
	t.Render()
}

// jobsTitle is the two-line table title: the cluster (or "Queue") over a job-count
// summary and the username.
func jobsTitle(cluster, user string, rows []JobRow) string {
	head := "Queue"
	if cluster != "" {
		head = cluster
	}
	var run, q, other int
	for _, r := range rows {
		switch r.State {
		case "running":
			run++
		case "queued":
			q++
		default:
			other++
		}
	}
	sub := fmt.Sprintf("%d jobs", len(rows))
	if len(rows) > 0 {
		var parts []string
		if run > 0 {
			parts = append(parts, fmt.Sprintf("%d running", run))
		}
		if q > 0 {
			parts = append(parts, fmt.Sprintf("%d queued", q))
		}
		if other > 0 {
			parts = append(parts, fmt.Sprintf("%d other", other))
		}
		sub += " (" + strings.Join(parts, ", ") + ")"
	}
	if user != "" && user != "?" {
		sub += "   " + user
	}
	return head + "\n" + sub
}

// jobStateBadge is the plain (uncolored) badge text; jobStateTransformer colors it
// without changing width, so go-pretty measures the real display width.
func jobStateBadge(state string) string {
	switch state {
	case "running":
		return glyph("●", "*") + " running"
	case "queued":
		return glyph("○", "o") + " queued"
	case "held":
		return glyph("!", "!") + " held"
	case "exiting":
		return glyph("◐", "~") + " exiting"
	case "complete":
		return glyph("✓", "+") + " complete"
	case "waiting":
		return glyph("○", "o") + " waiting"
	case "suspended":
		return glyph("!", "!") + " suspended"
	default:
		s := strings.TrimSpace(state)
		if s == "" {
			s = "?"
		}
		return glyph("·", ".") + " " + s
	}
}

func jobStateTransformer(v interface{}) string {
	s := fmt.Sprint(v)
	switch {
	case strings.Contains(s, "running"):
		return text.Colors{text.FgGreen}.Sprint(s)
	case strings.Contains(s, "held"), strings.Contains(s, "suspended"):
		return text.Colors{text.FgYellow}.Sprint(s)
	case strings.Contains(s, "exiting"), strings.Contains(s, "complete"):
		return text.Colors{text.FgCyan}.Sprint(s)
	default: // queued, waiting, unknown → quiet
		return text.Colors{text.FgHiBlack}.Sprint(s)
	}
}

// elapWall renders the elapsed/walltime cell: "elap / wall", or just elapsed when
// the format carries no requested walltime (narrow qstat).
func elapWall(elapsed, reqWall string) string {
	e := dash(elapsed)
	if strings.TrimSpace(reqWall) == "" {
		return e
	}
	return e + " / " + dash(reqWall)
}

// dash normalizes empty / "--" fields to a single "--" placeholder.
func dash(s string) string {
	if strings.TrimSpace(s) == "" || s == "--" {
		return "--"
	}
	return s
}

// truncRight trims to maxLen display columns, keeping the head (a job name's
// distinguishing prefix) behind a trailing ….
func truncRight(s string, maxLen int) string {
	if text.StringWidth(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	r := []rune(s)
	i, w := 0, 0
	for i < len(r) && w+text.StringWidth(string(r[i])) <= maxLen-1 {
		w += text.StringWidth(string(r[i]))
		i++
	}
	return string(r[:i]) + "…"
}
