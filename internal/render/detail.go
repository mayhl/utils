package render

import (
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// JobDetailView is one job's attributes for JobDetailCard — a plain mirror of
// queue.JobDetail so render stays domain-free (like JobRow). Empty fields are omitted
// from the card. Times are the scheduler's verbatim strings; the card formats them.
type JobDetailView struct {
	ID, Name, User, Account, Queue, State, RawState string
	Nodes, Tasks, Elapsed, ReqWall                  string
	Submit, Start, End, WorkDir, StdOut, StdErr     string
	ExitStatus, Reason, Cluster                     string
	Model                                           [][2]string // ordered model-hook key/values → a separated card section
}

// JobDetailCard renders one job's full detail as the house card: a titled rounded box
// (id · state badge · cluster) over label/value rows, each populated field on its own
// line (empty fields dropped). The state row reuses the queue palette badge + color, the
// walltime row the burn tint, and timestamps expand to a verbose "6 Jul 2026 00:00" form
// (tables stay compact; the card has room for the full date). Its `--raw`/`--json` twins
// live in the minfo command; this is the pretty default.
func JobDetailCard(d JobDetailView) {
	t := newDetailTable(d)
	t.SetOutputMirror(os.Stdout)
	t.Render()
}

// RenderJobDetailCard returns the house card as a string instead of printing it —
// used by the interactive picker's `i` inspect overlay. Same card as JobDetailCard.
func RenderJobDetailCard(d JobDetailView) string {
	return newDetailTable(d).Render()
}

// newDetailTable builds the detail card's table (title + label/value rows + column
// config) without an output target; callers print it or capture the rendered string.
func newDetailTable(d JobDetailView) table.Writer {
	t := table.NewWriter()
	applyStyle(t)
	t.SetTitle(detailTitle(d))

	labelW := 0
	// add appends a non-empty field, coloring the value with a palette hue (empty c =
	// default fg). Coloring is inline Sprint (respects applyStyle's DisableColors under
	// plain/NO_COLOR). Field hues are theme-adaptive Fg from the palette; green/yellow/red
	// stay reserved for the status fields below (Walltime burn, Exit verdict).
	add := func(label, value string, c text.Colors) {
		if strings.TrimSpace(value) == "" {
			return
		}
		labelW = max(labelW, len(label))
		if len(c) > 0 {
			value = c.Sprint(value)
		}
		t.AppendRow(table.Row{label, value})
	}

	add("Name", d.Name, nil)
	add("User", d.User, tc(HueUser)) // magenta
	add("Account", d.Account, nil)
	add("Queue", d.Queue, tc(HueGroup)) // bright-blue
	add("Nodes", nodesTasks(d.Nodes, d.Tasks), nil)
	add("Walltime", walltimeLine(d.State, d.Elapsed, d.ReqWall), nil) // status: pre-tinted by burn
	add("Submitted", longTime(d.Submit), nil)
	add("Started", longTime(d.Start), nil)
	add("Ended", longTime(d.End), nil)
	add("WorkDir", d.WorkDir, tc(HueLoc)) // blue (location)
	add("StdOut", d.StdOut, tc(HueLoc))
	add("StdErr", d.StdErr, tc(HueLoc))
	add("Exit", d.ExitStatus, exitColors(d.ExitStatus)) // status: green ok / red fail
	add("Reason", d.Reason, tc(HueWarn))                // pending-reason caution

	// Model-hook section: freeform metrics rendered verbatim under a divider —
	// simple dicts in, simple cards out.
	if len(d.Model) > 0 {
		t.AppendSeparator()
		for _, kv := range d.Model {
			add(kv[0], kv[1], nil)
		}
	}

	cols := []table.ColumnConfig{{Number: 1, Colors: tc(HueDim)}} // labels dim
	// Wrap the value column to the terminal so a long path (StdOut/StdErr/WorkDir)
	// continues on the next line, full value preserved, rather than overflowing the
	// card width. No cap when the width is unknown (piped) — full values flow, like the
	// tables' planFit. StyleRounded overhead for 2 cols ≈ 3*2+1.
	if tw := termWidth(); tw > 0 {
		valueMax := max(20, tw-labelW-7)
		cols = append(cols, table.ColumnConfig{Number: 2, WidthMax: valueMax, WidthMaxEnforcer: text.WrapText})
	}
	t.SetColumnConfigs(cols)
	return t
}

// KVField is one label/value line for the generic KVCard. Hue is a palette hue key
// (HueUser, HueLoc, …); "" leaves the value at the default fg. An empty Value drops the row.
type KVField struct {
	Label, Value, Hue string
}

// KVCard renders a generic label/value detail card in the house rounded box — the
// domain-free sibling of JobDetailCard, used by `mu log -i`'s inspect overlay. Empty
// values are dropped, labels are dim, and the value column wraps to the terminal so a
// long payload or path flows onto continuation lines instead of overflowing the card.
// title may carry its own ANSI (the caller styles it); an empty title omits the header.
func KVCard(title string, fields []KVField) string {
	t := table.NewWriter()
	applyStyle(t)
	if strings.TrimSpace(title) != "" {
		t.SetTitle(title)
	}
	labelW := 0
	for _, f := range fields {
		if strings.TrimSpace(f.Value) == "" {
			continue
		}
		labelW = max(labelW, len(f.Label))
		v := f.Value
		if f.Hue != "" {
			v = tc(f.Hue).Sprint(v)
		}
		t.AppendRow(table.Row{f.Label, v})
	}
	cols := []table.ColumnConfig{{Number: 1, Colors: tc(HueDim)}} // labels dim
	if tw := termWidth(); tw > 0 {
		cols = append(cols, table.ColumnConfig{Number: 2, WidthMax: max(20, tw-labelW-7), WidthMaxEnforcer: text.WrapText})
	}
	t.SetColumnConfigs(cols)
	return t.Render()
}

// detailTitle is the card's header: "Job <short> · <state badge> · <cluster>".
func detailTitle(d JobDetailView) string {
	id := d.ID
	if strings.TrimSpace(d.RawState) != "" || d.State != "" {
		// prefer the short id for the header if the full id is host-suffixed
		if i := strings.IndexByte(id, '.'); i > 0 {
			id = id[:i]
		}
	}
	parts := []string{"Job " + append(tc(HueID), text.Bold).Sprint(id)} // cyan (id)
	state := d.State
	if state == "unknown" || state == "" {
		state = strings.TrimSpace(d.RawState)
	}
	if state != "" {
		parts = append(parts, jobStateTransformer(jobStateBadge(d.State)))
	}
	if strings.TrimSpace(d.Cluster) != "" {
		parts = append(parts, append(tc(HueLoc), text.Bold).Sprint(d.Cluster)) // blue (location)
	}
	return strings.Join(parts, "   ·   ")
}

// exitColors tints the exit-status value: green for success (0 / 0:0), red for a
// non-zero exit or signal — the one field where color carries a verdict.
func exitColors(s string) text.Colors {
	switch strings.TrimSpace(s) {
	case "", "0", "0:0":
		return text.Colors{text.FgGreen}
	default:
		return text.Colors{text.FgRed}
	}
}

// nodesTasks renders the resource line: "4  (96 tasks)" when both are known, else
// whichever is present.
func nodesTasks(nodes, tasks string) string {
	nodes, tasks = strings.TrimSpace(nodes), strings.TrimSpace(tasks)
	switch {
	case nodes != "" && tasks != "":
		return nodes + "  (" + tasks + " tasks)"
	case nodes != "":
		return nodes
	default:
		return tasks
	}
}

// walltimeLine renders "elap / wall (NN%)" for a running job, tinted by burn; a
// finished/queued job shows the pair without the percentage.
func walltimeLine(state, elapsed, reqWall string) string {
	elapsed, reqWall = strings.TrimSpace(elapsed), strings.TrimSpace(reqWall)
	if elapsed == "" && reqWall == "" {
		return ""
	}
	line := dash(elapsed)
	if reqWall != "" {
		line += " / " + reqWall
	}
	if e, ok1 := durSecs(elapsed); ok1 {
		if w, ok2 := durSecs(reqWall); ok2 && w > 0 {
			line += " (" + pct(e, w) + ")"
		}
	}
	if lvl := walltimeLevel(state, elapsed, reqWall); lvl != "" {
		line = levelColors(lvl).Sprint(line)
	}
	return line
}

// pct is the integer percentage e/w as "NN%".
func pct(e, w int) string {
	return strconv.Itoa(e*100/w) + "%"
}

// longTime expands an ISO 8601 stamp "2006-01-02T15:04:05" to a verbose "2 Jan 2006
// 15:04" (year kept, seconds dropped) by fixed-offset slicing — no time.Parse, so no
// timezone shift (the value stays the cluster-local clock the scheduler reported).
// A non-ISO value (PBS's already-human string, "N/A", "Unknown") passes through.
func longTime(s string) string {
	s = strings.TrimSpace(s)
	switch s { // SLURM's "not yet" sentinels → blank so the card omits the row
	case "Unknown", "N/A", "None":
		return ""
	}
	if len(s) >= 16 && s[4] == '-' && s[7] == '-' && s[10] == 'T' && s[13] == ':' {
		mon := monthName(s[5:7])
		if mon != "" {
			day := strings.TrimPrefix(s[8:10], "0")
			return day + " " + mon + " " + s[0:4] + " " + s[11:16]
		}
	}
	return s
}

// monthName maps a two-digit month ("01".."12") to its short name, or "" if invalid.
func monthName(mm string) string {
	names := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	switch mm {
	case "01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12":
		i := int(mm[0]-'0')*10 + int(mm[1]-'0') - 1
		return names[i]
	}
	return ""
}
