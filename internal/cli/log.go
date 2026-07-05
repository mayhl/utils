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
	c := &cobra.Command{
		Use:   "log",
		Short: "View the event log (transfers, jobs, big ops).",
		Long:  "Show mu's event log. Subcommands: `write` (append, for scripts), `clear`.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return viewLog(tier, scope, since)
		},
	}
	f := c.Flags()
	f.StringVarP(&tier, "tier", "t", "", "only this tier (info|ok|warn|error)")
	f.StringVarP(&scope, "scope", "s", "", "only this scope (cp|hpc|job|…)")
	f.StringVar(&since, "since", "", "only newer than a duration (2h, 3d) or date (2026-07-01)")
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

func viewLog(tier, scope, since string) error {
	path := render.EventLogPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			render.Info("no events yet (" + path + ")")
			return nil
		}
		return err
	}
	defer f.Close()

	tier = strings.ToUpper(strings.TrimSpace(tier))
	var cutoff time.Time
	if since != "" {
		if cutoff, err = parseSince(since); err != nil {
			return err
		}
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	shown := 0
	for sc.Scan() {
		e, ok := parseLogLine(sc.Text())
		if !ok {
			continue
		}
		if tier != "" && !tierMatch(tier, e.level) {
			continue
		}
		if scope != "" && !strings.EqualFold(scope, e.scope) {
			continue
		}
		if !cutoff.IsZero() && !e.t.IsZero() && e.t.Before(cutoff) {
			continue
		}
		fmt.Println(render.FormatEntry(e.level, e.scope, e.rawTS, e.msg))
		shown++
	}
	if shown == 0 {
		render.Info("no matching events")
	}
	return sc.Err()
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
