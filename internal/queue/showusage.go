package queue

import (
	"regexp"
	"strings"
)

// UsageInfo is one row of `show_usage`: a subproject's allocation hours, burn, and
// remainder. Fields stay strings (like QueueInfo/StorageInfo) so blanks and non-numeric
// markers survive verbatim; PctRemain keeps the site's own "35.00%" form. Sibling site
// command of show_queues/show_storage — same ruler-table machinery.
type UsageInfo struct {
	System     string
	Subproject string
	Allocated  string
	Used       string
	Remaining  string
	PctRemain  string
	Background string
	// FYLeft is the banner-level percent-of-year-remaining ("33.70"), stamped by the
	// caller from ParseFiscalYearLeft — it's not a table column, but a collated row must
	// keep its own system's year context for the pace derivation.
	FYLeft string
}

// reFiscalYearPct matches the parenthesized "(XX.XX% …)" a show_usage banner line carries
// (around line 5) — the percent of the allocation (fiscal) year remaining. Tolerates
// trailing words inside the parens ("(33.70% of year remains)") and text after them;
// requiring the % inside parens keeps prose percents from matching.
var reFiscalYearPct = regexp.MustCompile(`\((\d+(?:\.\d+)?)\s*%[^)]*\)`)

// ParseFiscalYearLeft extracts the percent-of-year-remaining from show_usage's prose —
// "Hours Remaining in the Fiscal Year:  1985 (22.67%)", banner line ~5 — as the bare
// number ("22.67"), or "" when absent (callers degrade to skipping the pace column).
// The whole output is scanned, position-agnostic, so a system that moves the line below
// the table still parses; table cells never parenthesize their percents, so they can't
// false-match.
func ParseFiscalYearLeft(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		if m := reFiscalYearPct.FindStringSubmatch(ln); m != nil {
			return m[1]
		}
	}
	return ""
}

// ParseShowUsage parses the show_usage table. Like show_storage it anchors on the header
// (the line containing "Subproject") then the `=`-ruler under it — the banner can carry
// its own all-`=` divider lines — and reads rows until a blank line / EOF.
func ParseShowUsage(text string) []UsageInfo {
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && !strings.Contains(lines[i], "Subproject") {
		i++
	}
	for i < len(lines) && !isRuler(lines[i]) {
		i++
	}
	if i >= len(lines) {
		return nil
	}
	starts := columnStarts(lines[i])
	if len(starts) < 7 {
		return nil
	}
	var rows []UsageInfo
	for i++; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		c := sliceCols(lines[i], starts)
		rows = append(rows, UsageInfo{
			System: c[0], Subproject: c[1],
			Allocated: c[2], Used: c[3], Remaining: c[4],
			PctRemain: c[5], Background: c[6],
		})
	}
	return rows
}
