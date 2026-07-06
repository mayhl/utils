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
	ID, Name, User, Queue, Nodes, State, Elapsed, ReqWall, Reason string
	Cluster                                                       string // set only by cross-cluster collate → a leftmost Cluster column
}

// JobsTable renders a scheduler queue as the house table: ID / Name / Queue / NDS /
// State / Elap·Wall. The state cell is a glyph colored by state (● green running / ○
// dim queued / ! yellow held / …); the title carries the cluster + a job-count
// summary. Long names truncate on the right to keep the table from wrapping.
func JobsTable(cluster, user string, rows []JobRow) {
	showUser := multipleUsers(rows)
	showCluster := anyCluster(rows)
	nameMax, showReason, showWall := planFit(rows, showUser, showCluster)
	wallHdr := "Elap / Wall"
	if !showWall {
		wallHdr = "Elap"
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	t.SetTitle(jobsTitle(cluster, user, rows))

	header := table.Row{}
	if showCluster {
		header = append(header, "Cluster")
	}
	header = append(header, "ID", "Name")
	if showUser {
		header = append(header, "User")
	}
	header = append(header, "Queue", "NDS", "State", wallHdr)
	t.AppendHeader(header)

	for _, r := range rows {
		row := table.Row{}
		if showCluster {
			row = append(row, r.Cluster)
		}
		row = append(row, r.ID, r.Name)
		if showUser {
			row = append(row, r.User)
		}
		row = append(row, r.Queue, dash(r.Nodes), stateCell(r, showReason), elapWall(r.Elapsed, r.ReqWall, showWall))
		t.AppendRow(row)
	}

	// ColumnConfigs match by header name, so listing Cluster is harmless when the
	// column is absent (single-cluster views).
	cols := []table.ColumnConfig{
		{Name: "Cluster", Colors: text.Colors{text.FgCyan, text.Bold}},
		{Name: "ID", Colors: text.Colors{text.FgGreen, text.Bold}},
		{Name: "Name", WidthMaxEnforcer: truncRight},
		{Name: "User", Colors: text.Colors{text.FgBlue}},
		{Name: "Queue", Colors: text.Colors{text.FgMagenta}},
		{Name: "State", Transformer: jobStateTransformer},
	}
	if nameMax > 0 {
		cols[1].WidthMax = nameMax
	}
	t.SetColumnConfigs(cols)
	t.Render()
}

// multipleUsers reports whether the rows span more than one owner — the cue to add a
// User column (an all-users view) vs omit it (your own jobs are all you).
func multipleUsers(rows []JobRow) bool {
	seen := ""
	for _, r := range rows {
		u := strings.TrimSpace(r.User)
		if u == "" {
			continue
		}
		if seen == "" {
			seen = u
		} else if u != seen {
			return true
		}
	}
	return false
}

// anyCluster reports whether any row carries a cluster tag — the cue to add the
// leftmost Cluster column (a cross-cluster collate) vs omit it (single-cluster view).
func anyCluster(rows []JobRow) bool {
	for _, r := range rows {
		if strings.TrimSpace(r.Cluster) != "" {
			return true
		}
	}
	return false
}

// stateCell is the State column value: the state badge, plus a pending job's reason
// (SLURM — why it waits) when showReason survives the terminal fit.
func stateCell(r JobRow, showReason bool) string {
	s := jobStateBadge(r.State)
	if showReason && r.Reason != "" {
		s += " · " + r.Reason
	}
	return s
}

// planFit decides how the table fits the terminal: Name's max width (0 = uncapped)
// and whether the pending-reason and walltime survive. It sheds width in priority
// order — shrink Name to a floor, then drop the reason, then the walltime — so even a
// narrow terminal renders one clean, unwrapped table. It runs for both pretty and
// --plain (both are human views that fit the terminal), no-opping only when the width
// is unknown (piped/redirected), where full values flow — use --json for complete data.
func planFit(rows []JobRow, showUser, showCluster bool) (nameMax int, showReason, showWall bool) {
	showReason, showWall = true, true
	tw := termWidth()
	if tw <= 0 {
		return 0, true, true
	}
	const nameFloor = 6
	idW, queueW, ndsW := len("ID"), len("Queue"), len("NDS")
	badgeW, elapW, nameW := len("State"), len("Elap"), len("Name")
	userW, clusterW, reasonW, wallW := 0, 0, 0, 0
	if showUser {
		userW = len("User")
	}
	if showCluster {
		clusterW = len("Cluster")
	}
	for _, r := range rows {
		idW = max(idW, text.StringWidth(r.ID))
		queueW = max(queueW, text.StringWidth(r.Queue))
		ndsW = max(ndsW, text.StringWidth(dash(r.Nodes)))
		badgeW = max(badgeW, text.StringWidth(jobStateBadge(r.State)))
		elapW = max(elapW, text.StringWidth(dash(r.Elapsed)))
		nameW = max(nameW, text.StringWidth(r.Name))
		if showUser {
			userW = max(userW, text.StringWidth(r.User))
		}
		if showCluster {
			clusterW = max(clusterW, text.StringWidth(r.Cluster))
		}
		if r.Reason != "" {
			reasonW = max(reasonW, text.StringWidth(" · "+r.Reason))
		}
		if strings.TrimSpace(r.ReqWall) != "" {
			wallW = max(wallW, text.StringWidth(" / "+r.ReqWall))
		}
	}
	// StyleRounded overhead: 2 padding + a border glyph per column → 3*ncols + 1.
	nCols := 6
	if showUser {
		nCols++
	}
	if showCluster {
		nCols++
	}
	room := tw - (idW + userW + clusterW + queueW + ndsW + badgeW + elapW + 3*nCols + 1) // for Name + reason + wall
	nameMax = nameW
	for {
		need := nameMax + boolW(showReason, reasonW) + boolW(showWall, wallW)
		if need <= room {
			break
		}
		switch {
		case nameMax > nameFloor:
			nameMax = max(nameFloor, room-boolW(showReason, reasonW)-boolW(showWall, wallW))
		case showReason:
			showReason = false
		case showWall:
			showWall = false
		default:
			return nameFloor, false, false // can't shrink more — accept a slight overflow
		}
	}
	if nameMax >= nameW {
		nameMax = 0 // no cap needed
	}
	return nameMax, showReason, showWall
}

// boolW returns w when on, else 0 — for conditional width sums.
func boolW(on bool, w int) int {
	if on {
		return w
	}
	return 0
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
	default: // queued, waiting, unknown → terminal default fg (always legible, unlike a dim gray)
		return s
	}
}

// elapWall renders the elapsed/walltime cell: "elap / wall", or just elapsed when
// the format carries no requested walltime (narrow qstat).
func elapWall(elapsed, reqWall string, showWall bool) string {
	e := dash(elapsed)
	if !showWall || strings.TrimSpace(reqWall) == "" {
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
