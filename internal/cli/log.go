package cli

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/render"
)

func logCmd() *cobra.Command {
	var tier, scope, since string
	var lines int
	var all, interactive bool
	c := &cobra.Command{
		Use:   "log",
		Short: "View the event log (transfers, jobs, big ops).",
		Long: "Show mu's event log, newest last, grouped by day. Defaults to the last 50\n" +
			"events (-n to change, --all for the whole log). -i opens a live, scrollable,\n" +
			"filterable viewer. Subcommands: `write`, `clear`.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if interactive {
				return interactiveLog(tier, scope, since)
			}
			return viewLog(tier, scope, since, lines, all)
		},
	}
	f := c.Flags()
	f.StringVarP(&tier, "tier", "t", "", "only this tier (info|ok|warn|error)")
	f.StringVarP(&scope, "scope", "s", "", "only this scope (cp|hpc|job|…)")
	f.StringVar(&since, "since", "", "only newer than a duration (2h, 3d) or date (2026-07-01)")
	f.IntVarP(&lines, "lines", "n", 50, "show only the last N events (0 = all)")
	f.BoolVar(&all, "all", false, "show the entire log (overrides -n)")
	f.BoolVarP(&interactive, "interactive", "i", false, "browse in a live, scrollable, filterable viewer")
	c.AddCommand(logWriteCmd(), logClearCmd())
	return c
}

func logWriteCmd() *cobra.Command {
	var scope string
	c := &cobra.Command{
		Use:   "write <level> <msg>",
		Short: "Append an event to the log (for external scripts).",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			render.WriteEvent(scope, args[0], strings.Join(args[1:], " "))
			return nil
		},
	}
	c.Flags().StringVarP(&scope, "scope", "s", "ext", "event scope tag")
	return c
}

func logClearCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "clear",
		Short: "Truncate the event log.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path := render.EventLogPath()
			if !yes {
				fmt.Fprintf(os.Stderr, "clear %s? [y/N] ", path)
				var r string
				_, _ = fmt.Scanln(&r)
				if strings.ToLower(strings.TrimSpace(r)) != "y" {
					render.Info("aborted")
					return nil
				}
			}
			if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
				return err
			}
			render.OK("event log cleared")
			return nil
		},
	}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return c
}

// --- reader ---

type logEntry struct {
	t          time.Time
	rawTS      string
	level      string
	scope, msg string
}

var (
	leadBracketRE = regexp.MustCompile(`^\[([^\]]*)\]\s*`)
	scopeTokenRE  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

// parseLogLine tolerantly parses `[ts] [level] [scope] msg` (Go) and the older
// 2-field `[ts] [level] msg`: grabs up to three leading [..] groups; the third is
// scope only if it looks like a scope token, else it's part of the message.
func parseLogLine(line string) (logEntry, bool) {
	rest := line
	var fields []string
	for len(fields) < 3 {
		m := leadBracketRE.FindStringSubmatch(rest)
		if m == nil {
			break
		}
		fields = append(fields, strings.TrimSpace(m[1]))
		rest = rest[len(m[0]):]
	}
	if len(fields) < 2 {
		return logEntry{}, false
	}
	e := logEntry{rawTS: fields[0], level: strings.ToUpper(fields[1])}
	switch {
	case len(fields) == 3 && scopeTokenRE.MatchString(fields[2]):
		e.scope, e.msg = fields[2], rest
	case len(fields) == 3:
		e.msg = "[" + fields[2] + "] " + rest // third bracket was part of the message
	default:
		e.msg = rest
	}
	if ts, err := time.ParseInLocation("2006-01-02T15:04:05", e.rawTS, time.Local); err == nil {
		e.t = ts
	}
	return e, true
}

func viewLog(tier, scope, since string, limit int, all bool) error {
	tierU := strings.ToUpper(strings.TrimSpace(tier))
	cutoff, err := sinceCutoff(since)
	if err != nil {
		return err
	}
	rows, err := readLog(tierU, scope, cutoff)
	if err != nil {
		if os.IsNotExist(err) {
			render.Info("no events yet (" + render.EventLogPath() + ")")
			return nil
		}
		return err
	}

	total := len(rows)
	if total == 0 {
		render.Info("no matching events")
		return nil
	}
	title := fmt.Sprintf("Event log · %d event%s", total, plural(total))
	if !all && limit > 0 && total > limit {
		rows = rows[total-limit:]
		title = fmt.Sprintf("Event log · last %d of %d", limit, total)
	}
	fmt.Println(render.LogBlock(title, rows))
	return nil
}

// readLog reads and filters the event log into rows (oldest first). tierU must be
// already upper-cased; cutoff zero means no since-bound. A missing log surfaces
// os.ErrNotExist so callers can distinguish "no log yet" from "no matches".
func readLog(tierU, scope string, cutoff time.Time) ([]render.LogRow, error) {
	f, err := os.Open(render.EventLogPath())
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var rows []render.LogRow
	for sc.Scan() {
		e, ok := parseLogLine(sc.Text())
		if !ok {
			continue
		}
		if tierU != "" && !tierMatch(tierU, e.level) {
			continue
		}
		if scope != "" && !strings.EqualFold(scope, e.scope) {
			continue
		}
		if !cutoff.IsZero() && !e.t.IsZero() && e.t.Before(cutoff) {
			continue
		}
		rows = append(rows, render.LogRow{Time: e.t, RawTS: e.rawTS, Level: e.level, Scope: e.scope, Msg: e.msg})
	}
	return rows, sc.Err()
}

// sinceCutoff resolves a --since string to a cutoff time (zero when empty).
func sinceCutoff(since string) (time.Time, error) {
	if since == "" {
		return time.Time{}, nil
	}
	return parseSince(since)
}

// interactiveLog opens the read-only viewer (`mu log -i`): a live, scrollable,
// filterable flat table over the same tier/scope/since filters. Re-reads on the
// widget's refresh tick so new events tail in.
func interactiveLog(tier, scope, since string) error {
	if !render.Interactive() {
		return fmt.Errorf("mu log -i needs a terminal (stdin is not a tty)")
	}
	tierU := strings.ToUpper(strings.TrimSpace(tier))
	cutoff, err := sinceCutoff(since)
	if err != nil {
		return err
	}
	initial, err := readLog(tierU, scope, cutoff)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(initial) == 0 {
		render.Info("no matching events")
		return nil
	}
	return render.Viewer(render.SelectSpec{
		Title:    "Event log",
		Columns:  []string{"TIME", "LVL", "SCOPE", "MESSAGE"},
		Interval: 2 * time.Second,
		Fetch: func() []render.SelectRow {
			rows, _ := readLog(tierU, scope, cutoff) // tolerate a blip; keep the last frame
			return logSelectRows(rows)
		},
	})
}

// logSelectRows adapts log rows into viewer rows: TIME (date+time — the flat view
// has no day headers) · LVL glyph · SCOPE · MESSAGE. The row ID is the stamp+message
// so the cursor holds its place across a live refresh. Hues: time blue, glyph
// tier-colored, scope magenta, message default (matches the static view).
func logSelectRows(rows []render.LogRow) []render.SelectRow {
	out := make([]render.SelectRow, len(rows))
	for i, r := range rows {
		tm := r.RawTS
		if !r.Time.IsZero() {
			tm = r.Time.Format("01-02 15:04:05")
		}
		g, hue := logLevelGlyphHue(r.Level)
		out[i] = render.SelectRow{
			ID:    tm + " " + r.Msg,
			Cells: []string{tm, g, r.Scope, r.Msg},
			Hues:  []string{render.HueLoc, hue, render.HueUser, ""},
		}
	}
	return out
}

// logLevelGlyphHue maps a level to its glyph and house hue key for the viewer's LVL
// cell (the glyph carries the level; hue is status-reserved green/yellow/red/cyan).
func logLevelGlyphHue(level string) (glyph, hue string) {
	switch strings.ToUpper(level) {
	case "OK":
		return "✓", render.HueOK
	case "WARN", "WARNING":
		return "!", render.HueWarn
	case "ERROR", "ERR":
		return "✗", render.HueErr
	default:
		return "→", render.HueID
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// tierMatch compares tiers, accepting warn/warning and err/error.
func tierMatch(want, have string) bool {
	norm := func(s string) string {
		switch s {
		case "WARNING":
			return "WARN"
		case "ERR":
			return "ERROR"
		}
		return s
	}
	return norm(want) == norm(have)
}

func parseSince(s string) (time.Time, error) {
	if d, ok := parseDur(s); ok {
		return time.Now().Add(-d), nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad --since %q (use 2h/3d or 2026-07-01)", s)
}

// parseDur extends time.ParseDuration with a 'd' (days) suffix.
func parseDur(s string) (time.Duration, bool) {
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil {
			return time.Duration(n) * 24 * time.Hour, true
		}
		return 0, false
	}
	d, err := time.ParseDuration(s)
	return d, err == nil
}
