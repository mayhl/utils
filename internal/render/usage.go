package render

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// UsageRow is one subproject allocation for UsageTable — plain preformatted fields, so
// render stays domain-free. RemainPct keeps the site's own "35.00%" form; VsFY is the
// signed percentage-point margin over the fiscal-year pace ("+26.3%" / "-23.7%"), "" when
// the banner percent wasn't parsed. FYLeft is the bare year percent ("22.67") the row
// paces against — per row, since collated systems each carry their own banner. System is
// the owning cluster in a collate view. In the grouped collate layout a repeated
// Subproject arrives blanked (group continuation), and Total marks a per-subproject
// cross-system sum row (rendered bold).
type UsageRow struct {
	System, Subproject, Allocated, Used, Remaining, RemainPct, Background, VsFY, FYLeft string
	Total                                                                               bool
}

// usageCol is one renderable usage-table column: its header and a row→cell formatter.
type usageCol struct {
	header string
	cell   func(UsageRow) string
}

// UsageTable renders per-subproject allocation usage (show_usage) as the house table:
// [System] / Subproject / Allocated / Used / Remaining / Remain% / [Background] / [vs FY].
// fyLeft (bare "33.70", possibly "") lands in the title, not a column — it's one number
// for the whole system. System appears only in a collate view, Background only when some
// row reports background hours, vs FY only when a banner percent parsed.
func UsageTable(cluster, fyLeft string, rows []UsageRow) {
	cols := planUsageCols(rows)
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	title := fmt.Sprintf("%s — usage", cluster)
	if fyLeft != "" {
		title += fmt.Sprintf(" · FY %s%% left", fyLeft)
	}
	t.SetTitle("%s", title) // SetTitle Sprintf's its arg — the title's own % must not re-format
	header := make(table.Row, len(cols))
	for i, c := range cols {
		header[i] = c.header
	}
	t.AppendHeader(header)
	// Grouped collate layout: a blanked Subproject marks a group continuation, so a
	// non-blank one after the first row starts a new group → divider. A table with no
	// blanked rows (single-cluster, or nothing repeated) draws no dividers.
	grouped := false
	for _, r := range rows {
		if r.Subproject == "" {
			grouped = true
			break
		}
	}
	for i, r := range rows {
		if grouped && i > 0 && r.Subproject != "" {
			t.AppendSeparator()
		}
		row := make(table.Row, len(cols))
		for j, c := range cols {
			cell := c.cell(r)
			if r.Total {
				cell = text.Bold.Sprint(cell)
			}
			row[j] = cell
		}
		t.AppendRow(row)
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Name: "System", Colors: tc(HueUser)}, // magenta — stands out from the blue Subproject
		{Name: "Subproject", Colors: append(tc(HueGroup), text.Bold)},
		{Name: "Allocated", Colors: tc(HueDim)},
	})
	t.Render()
}

// planUsageCols builds the ordered columns. Subproject always leads — the collate view is
// grouped by subproject code, so System (present only there) slots second, telling apart
// the systems an allocation spans. Background appears only when some row reports
// background hours (the all-0/blank column is noise, same rule as the storage quota
// pairs); vs FY appears only when a fiscal-year percent was parsed for some row.
func planUsageCols(rows []UsageRow) []usageCol {
	cols := []usageCol{
		{"Subproject", func(r UsageRow) string { return r.Subproject }},
	}
	for _, r := range rows {
		if r.System != "" {
			cols = append(cols, usageCol{"System", func(r UsageRow) string { return dash(r.System) }})
			break
		}
	}
	cols = append(
		cols,
		usageCol{"Allocated", func(r UsageRow) string { return dash(r.Allocated) }},
		usageCol{"Used", func(r UsageRow) string { return dash(r.Used) }},
		usageCol{"Remaining", func(r UsageRow) string { return dash(r.Remaining) }},
		usageCol{"Remain%", remainCell},
	)
	if anyReported(rows, func(r UsageRow) string { return r.Background }) {
		cols = append(cols, usageCol{"Background", func(r UsageRow) string { return dash(r.Background) }})
	}
	for _, r := range rows {
		if r.VsFY != "" {
			cols = append(cols, usageCol{"vs FY", vsFYCell})
			break
		}
	}
	return cols
}

// anyReported reports whether any row carries a real (non-blank, non-zero) value in the
// given field. Values arrive preformatted, so zero is "0" (counts) or "0B" (sizes).
func anyReported[T any](rows []T, get func(T) string) bool {
	for _, r := range rows {
		switch strings.TrimSpace(get(r)) {
		case "", "0", "0B":
		default:
			return true
		}
	}
	return false
}

// UsageRemain grades an allocation's percent REMAINING into a label + house hue. BOTH
// ends are failure: too little left is exhaustion (≤10% HueErr, ≤25% HueWarn), and too
// much left this late in the year is forfeiture — allocations are use-it-or-lose-it, so
// hours that can't plausibly be burned before the year ends are as lost as hours already
// spent. Exhaustion is graded first (a nearly-empty allocation has nothing worth
// forfeiting); the surplus case is marked with a rising glyph so the two warm cells are
// told apart by shape, not color. A blank/non-numeric percent renders "--" (HueDim).
// fyLeft may be "" (no banner percent) — then only the exhaustion grade applies.
func UsageRemain(pct, fyLeft string) (label, hue string) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(pct), "%"), 64)
	if err != nil {
		return "--", HueDim
	}
	switch {
	case n <= 10:
		return pct, HueErr
	case n <= 25:
		return pct, HueWarn
	}
	if lvl := usageUnderuse(pct, fyLeft); lvl > 0 {
		return pct + " " + surplusGlyph(), underuseHue(lvl)
	}
	return pct, HueOK
}

func remainCell(r UsageRow) string {
	label, hue := UsageRemain(r.RemainPct, r.FYLeft)
	return tc(hue).Sprint(label)
}

// UsagePace grades the vs-FY margin (allocation-remaining minus year-remaining, in
// percentage points): a negative margin is overuse (down to -10 HueWarn, deeper HueErr).
// A POSITIVE margin is only good up to a point — past the forfeit threshold it is the
// use-it-or-lose-it warning, graded by the same burn multiple as Remain% and marked with
// the same rising glyph. "" (no banner percent) renders "--" (HueDim).
func UsagePace(vsFY, remainPct, fyLeft string) (label, hue string) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(vsFY), "%"), 64)
	if err != nil {
		return "--", HueDim
	}
	switch {
	case n < -10:
		return vsFY, HueErr
	case n < 0:
		return vsFY, HueWarn
	}
	if lvl := usageUnderuse(remainPct, fyLeft); lvl > 0 {
		return vsFY + " " + surplusGlyph(), underuseHue(lvl)
	}
	return vsFY, HueOK
}

func vsFYCell(r UsageRow) string {
	label, hue := UsagePace(r.VsFY, r.RemainPct, r.FYLeft)
	return tc(hue).Sprint(label)
}

// usageUnderuse grades use-it-or-lose-it risk from the BURN MULTIPLE — the percent of the
// allocation still unspent over the percent of the fiscal year still left. It answers
// "how many times the even pace must I now spend at to finish the allocation?": 40% left
// with 10% of the year to go = 4×, implausible; the same 40% with 80% of the year left is
// 0.5×, comfortable. So the multiple, not the raw margin, is what tightens as the year
// closes. ≥3× is at-risk (2 = HueErr), ≥2× worth flagging (1 = HueWarn), else 0.
// Allocations under a quarter unspent are never flagged — the forfeit is too small to
// chase, and the exhaustion grade already owns that band.
func usageUnderuse(remainPct, fyLeft string) int {
	r, err1 := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(remainPct), "%"), 64)
	f, err2 := strconv.ParseFloat(strings.TrimSpace(fyLeft), 64)
	if err1 != nil || err2 != nil || f <= 0 || r <= 25 {
		return 0
	}
	switch m := r / f; {
	case m >= 3:
		return 2
	case m >= 2:
		return 1
	default:
		return 0
	}
}

func underuseHue(level int) string {
	if level >= 2 {
		return HueErr
	}
	return HueWarn
}

// surplusGlyph marks the use-it-or-lose-it cells — the shape (not the hue) is what tells
// a hoarded allocation from a nearly-spent one, since both render warm.
func surplusGlyph() string {
	if asciiMode() {
		return "^"
	}
	return "↑"
}
