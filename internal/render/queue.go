package render

import (
	"fmt"
	"os"
	"strconv"
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
	Submit, Start, End                                            string // scheduler timestamps; shown as optional time columns per JobCols
	Cluster                                                       string // collate source tag (cluster or fleet node) → the leftmost System column
}

// JobCols selects which optional time columns JobsTable shows. Start is the live
// queue's est/actual start (mstat --start); Submit/End are finished-job stamps (mhist:
// End by default, Submit+Start+End under --times). Each is a fixed opt-in column —
// never shed by the terminal fit, it just consumes width budget.
type JobCols struct {
	Submit, Start, End bool
}

// JobsTable renders a scheduler queue as the house table: ID / Name / Queue / NDS /
// State / Elap·Wall. The state cell is a glyph colored by state (● green running / ○
// dim queued / ! yellow held / …); the title carries the cluster + a job-count
// summary. Long names truncate on the right to keep the table from wrapping.
func JobsTable(cluster, user string, rows []JobRow, cols JobCols) {
	showUser := multipleUsers(rows)
	showCluster := anyCluster(rows)
	nameMax, showReason, showWall := planFit(rows, showUser, showCluster, cols)
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
		header = append(header, "System")
	}
	header = append(header, "ID", "Name")
	if showUser {
		header = append(header, "User")
	}
	header = append(header, "Queue", "NDS", "State", wallHdr)
	if cols.Submit {
		header = append(header, "Submit")
	}
	if cols.Start {
		header = append(header, "Start")
	}
	if cols.End {
		header = append(header, "End")
	}
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
		cell := elapWall(r.Elapsed, r.ReqWall, showWall)
		if lvl := walltimeLevel(r.State, r.Elapsed, r.ReqWall); lvl != "" {
			cell = levelColors(lvl).Sprint(cell) // Sprint no-ops under DisableColors (plain/NO_COLOR)
		}
		row = append(row, r.Queue, dash(r.Nodes), stateCell(r, showReason), cell)
		if cols.Submit {
			row = append(row, timeCell(r.Submit))
		}
		if cols.Start {
			row = append(row, timeCell(r.Start))
		}
		if cols.End {
			row = append(row, timeCell(r.End))
		}
		t.AppendRow(row)
	}

	// ColumnConfigs match by header name, so listing a column is harmless when it's
	// absent (single-cluster / no-time-column views).
	const nameCol = 2 // index of the Name config below (System, ID, Name, …)
	colCfg := []table.ColumnConfig{
		{Name: "System", Colors: text.Colors{text.FgCyan, text.Bold}},
		{Name: "ID", Colors: text.Colors{text.FgGreen, text.Bold}},
		{Name: "Name", WidthMaxEnforcer: truncRight},
		{Name: "User", Colors: text.Colors{text.FgBlue}},
		{Name: "Queue", Colors: text.Colors{text.FgMagenta}},
		{Name: "State", Transformer: jobStateTransformer},
		{Name: "Submit", Colors: text.Colors{text.FgHiBlack}},
		{Name: "Start", Colors: text.Colors{text.FgHiBlack}},
		{Name: "End", Colors: text.Colors{text.FgHiBlack}},
	}
	if nameMax > 0 {
		colCfg[nameCol].WidthMax = nameMax // cap Name (truncRight enforces it); ColumnConfigs match by Name so slice order is otherwise cosmetic
	}
	t.SetColumnConfigs(colCfg)
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
// and whether the pending-reason and walltime survive. Name is first capped at a hard
// nameCap (so one long job name can't dominate even a wide terminal), then it sheds width
// in priority order — shrink Name to a floor, then drop the reason, then the walltime — so
// even a narrow terminal renders one clean, unwrapped table. It runs for both pretty and
// --plain (both are human views that fit the terminal), no-opping only when the width
// is unknown (piped/redirected), where full values flow — use --json for complete data.
func planFit(rows []JobRow, showUser, showCluster bool, cols JobCols) (nameMax int, showReason, showWall bool) {
	showReason, showWall = true, true
	tw := termWidth()
	if tw <= 0 {
		return 0, true, true
	}
	const nameFloor = 6
	const nameCap = 20 // hard ceiling on Name regardless of terminal room; longer names truncate
	idW, queueW, ndsW := len("ID"), len("Queue"), len("NDS")
	badgeW, elapW, nameW := len("State"), len("Elap"), len("Name")
	userW, clusterW, reasonW, wallW := 0, 0, 0, 0
	submitW, startW, endW := hdrW(cols.Submit, "Submit"), hdrW(cols.Start, "Start"), hdrW(cols.End, "End")
	if showUser {
		userW = len("User")
	}
	if showCluster {
		clusterW = len("System")
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
		if cols.Submit {
			submitW = max(submitW, text.StringWidth(timeCell(r.Submit)))
		}
		if cols.Start {
			startW = max(startW, text.StringWidth(timeCell(r.Start)))
		}
		if cols.End {
			endW = max(endW, text.StringWidth(timeCell(r.End)))
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
	nCols += boolN(cols.Submit) + boolN(cols.Start) + boolN(cols.End)
	// Time columns are opt-in, so they're fixed (never shed) — they just consume budget.
	room := tw - (idW + userW + clusterW + queueW + ndsW + badgeW + elapW + submitW + startW + endW + 3*nCols + 1) // for Name + reason + wall
	nameMax = min(nameW, nameCap)
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

// boolN counts a flag as a column (1 when on, else 0).
func boolN(on bool) int {
	if on {
		return 1
	}
	return 0
}

// hdrW seeds a time column's width with its header when shown, else 0.
func hdrW(on bool, header string) int {
	if on {
		return len(header)
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

// walltimeLevel grades a running job by how much of its requested walltime it has
// burned: "error" at/over 90% (imminent walltime kill — checkpoint now), "warn" at
// ≥75%, else "" (no color). Only running jobs are graded: a queued job hasn't
// started, and a finished one is moot. Feeds levelColors so the Elap/Wall cell
// reuses the house warn-yellow / error-red.
func walltimeLevel(state, elapsed, reqWall string) string {
	if state != "running" {
		return ""
	}
	e, ok1 := durSecs(elapsed)
	w, ok2 := durSecs(reqWall)
	if !ok1 || !ok2 || w <= 0 {
		return ""
	}
	switch r := float64(e) / float64(w); {
	case r >= 0.90:
		return "error"
	case r >= 0.75:
		return "warn"
	default:
		return ""
	}
}

// durSecs parses a scheduler duration into seconds, anchored on the right (rightmost
// field = seconds): SLURM "[D-]HH:MM:SS" / "MM:SS" parse exactly, and PBS "HH:MM"
// comes out 60× low — but walltimeLevel only uses the elapsed/limit ratio, which is
// scale-invariant when both share a format (they do within one job), so the ratio is
// always right. Returns ok=false for non-numeric limits ("UNLIMITED", "--", "").
func durSecs(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" {
		return 0, false
	}
	days := 0
	if i := strings.IndexByte(s, '-'); i > 0 { // SLURM day form "D-HH:MM:SS"
		d, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, false
		}
		days, s = d, s[i+1:]
	}
	secs := 0
	for _, f := range strings.Split(s, ":") {
		n, err := strconv.Atoi(f)
		if err != nil {
			return 0, false
		}
		secs = secs*60 + n
	}
	return days*86400 + secs, true
}

// timeCell formats a scheduler timestamp for a compact table time column (Submit /
// Start / End): an ISO 8601 stamp "2006-01-02T15:04:05" collapses to "01-02 15:04"
// (drop year + seconds — tables are length-constrained; the full date lives in the
// minfo card), while a non-ISO value a scheduler may emit ("N/A" unschedulable,
// "Unknown") or an empty/unavailable field passes through dash().
func timeCell(s string) string {
	s = strings.TrimSpace(s)
	// ISO 8601 YYYY-MM-DDTHH:MM:SS — slice MM-DD and HH:MM by fixed offsets.
	if len(s) >= 16 && s[4] == '-' && s[7] == '-' && s[10] == 'T' && s[13] == ':' {
		return s[5:10] + " " + s[11:16]
	}
	return dash(s)
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
