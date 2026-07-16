package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// hoursPreflight surfaces a submit's estimated core-hour cost before the confirm and warns,
// softly, when it crosses [job] hours_warn. It shows the estimate three ways — the absolute
// hours, and (when the target's allocation is known) the share of the total and of what
// remains. Advisory only: it never blocks a submit and never errors. estOverride (>0, from
// --hours) stands in for the script parse. mayFetch gates the live show_usage call: false on
// a dry run (stay offline — the cached number, if any, still shows), true on a real submit.
// `mu job sub` and `mu project submit` call it; `mu job tunnel` is exempt (trivial hours).
func hoursPreflight(node, account, script string, estOverride float64, mayFetch bool) {
	if node == "" {
		node, _ = currentCluster() // on-cluster `job sub` targets the local machine
	}
	est := estOverride
	basis := "--hours"
	if est <= 0 {
		var ok bool
		if est, basis, ok = estimateCoreHours(script, node); !ok {
			return // can't estimate and no --hours — nothing honest to say
		}
	}

	line := fmt.Sprintf("hours:   est %s core-hours (%s)", fmtHours(est), basis)
	if alloc, remain, stamp, ok := allocationHours(node, account, est, mayFetch); ok {
		var parts []string
		if alloc > 0 {
			parts = append(parts, fmt.Sprintf("%.1f%% of allocated", est/alloc*100))
		}
		if remain > 0 {
			parts = append(parts, fmt.Sprintf("%.1f%% of remaining", est/remain*100))
		}
		if len(parts) > 0 {
			line += " · " + strings.Join(parts, " · ")
		}
		if stamp != "" {
			line += " " + stamp
		}
	}
	render.Detail(line)

	if warn := config.HoursWarn(); warn > 0 && est > float64(warn) {
		render.Warn(fmt.Sprintf("estimated %s core-hours exceeds the %s-hour warn threshold ([job] hours_warn = 0 to silence)", fmtHours(est), fmtHours(float64(warn))))
	}
}

// allocationHours resolves node's allocation for account to (allocated, remaining) core-hours,
// with a staleness stamp for the caller to print. It prefers a fresh cached show_usage (under
// usageCacheTTL), but re-fetches live when the cache is stale OR when the estimate would eat a
// large share of what's cached-remaining — at an allocation's edge a stale number is exactly
// what misleads. mayFetch=false (dry run / no ticket) pins it to the cache; a fetch that can't
// reach the cluster degrades to the stale cache, then to nothing. ok=false with no account (no
// row to match) or no data at all — the percentages simply don't print.
func allocationHours(node, account string, est float64, mayFetch bool) (alloc, remain float64, stamp string, ok bool) {
	if strings.TrimSpace(account) == "" {
		return 0, 0, "", false
	}
	now := time.Now()
	rows, age, cached := readUsageCache(node, now)
	fresh := cached && age < usageCacheTTL
	if fresh {
		if _, r, found := matchUsage(rows, account); found && r > 0 && est > 0.9*r {
			fresh = false // near the edge — don't trust the age
		}
	}
	if !fresh && mayFetch {
		if live, lok := fetchUsageRows(node); lok {
			writeUsageCache(node, live)
			rows, cached, age = live, true, 0
		}
	}
	if !cached {
		return 0, 0, "", false
	}
	a, r, found := matchUsage(rows, account)
	if !found {
		return 0, 0, "", false
	}
	if age < time.Minute {
		stamp = "(usage just now)"
	} else {
		stamp = fmt.Sprintf("(usage %s old)", fmtAge(age))
	}
	return a, r, stamp, true
}

// matchUsage finds account's row among show_usage rows and parses its allocated/remaining
// hours. Match is case-insensitive on the Subproject (the allocation code). ok=false when no
// row matches or neither number parses.
func matchUsage(rows []queue.UsageInfo, account string) (alloc, remain float64, ok bool) {
	acct := strings.ToLower(strings.TrimSpace(account))
	for _, r := range rows {
		if strings.ToLower(strings.TrimSpace(r.Subproject)) != acct {
			continue
		}
		a, aok := parseHoursNum(r.Allocated)
		rm, rok := parseHoursNum(r.Remaining)
		if !aok && !rok {
			return 0, 0, false
		}
		return a, rm, true
	}
	return 0, 0, false
}

// fetchUsageRows runs show_usage live on node and parses it — the same fetch `mu hpc usage`
// makes. false when node is unnamed, the command fails, or nothing parsed.
func fetchUsageRows(node string) ([]queue.UsageInfo, bool) {
	if node == "" {
		return nil, false
	}
	_, out, err := fetchSite(node, showUsageCmd)
	if err != nil {
		return nil, false
	}
	rows := parseUsageWithFY(out)
	if len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

// parseHoursNum reads a show_usage hours cell — a plain number, tolerating thousands commas.
func parseHoursNum(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// fmtHours renders a core-hour count as a whole number with thousands separators — the
// estimate is a ceiling, so sub-hour precision would be false comfort.
func fmtHours(h float64) string {
	n := int64(h + 0.5)
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// fmtAge humanizes a cache age for the staleness stamp.
func fmtAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
