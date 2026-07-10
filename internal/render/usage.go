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
// the banner percent wasn't parsed. System is the owning cluster in a collate view. In
// the grouped collate layout a repeated Subproject arrives blanked (group continuation),
// and Total marks a per-subproject cross-system sum row (rendered bold).
type UsageRow struct {
	System, Subproject, Allocated, Used, Remaining, RemainPct, Background, VsFY string
	Total                                                                       bool
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
		usageCol{"Remain%", func(r UsageRow) string { return remainCell(r.RemainPct) }},
	)
	if anyReported(rows, func(r UsageRow) string { return r.Background }) {
		cols = append(cols, usageCol{"Background", func(r UsageRow) string { return dash(r.Background) }})
	}
	for _, r := range rows {
		if r.VsFY != "" {
			cols = append(cols, usageCol{"vs FY", func(r UsageRow) string { return vsFYCell(r.VsFY) }})
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

// UsageRemain grades an allocation's percent REMAINING into a label + house hue — low
// headroom is the warm direction: ≤10% HueErr, ≤25% HueWarn, else HueOK; a blank or
// non-numeric percent renders "--" (HueDim). Takes the site's own "35.00%" form.
func UsageRemain(pct string) (label, hue string) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(pct), "%"), 64)
	if err != nil {
		return "--", HueDim
	}
	switch {
	case n <= 10:
		return pct, HueErr
	case n <= 25:
		return pct, HueWarn
	default:
		return pct, HueOK
	}
}

func remainCell(pct string) string {
	label, hue := UsageRemain(pct)
	return tc(hue).Sprint(label)
}

// UsagePace grades the vs-FY margin (allocation-remaining minus year-remaining, in
// percentage points): ≥0 on/under budget (HueOK), a small overrun down to -10 HueWarn,
// deeper HueErr — the overuse warning. "" (no banner percent) renders "--" (HueDim).
func UsagePace(vsFY string) (label, hue string) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(vsFY), "%"), 64)
	if err != nil {
		return "--", HueDim
	}
	switch {
	case n >= 0:
		return vsFY, HueOK
	case n >= -10:
		return vsFY, HueWarn
	default:
		return vsFY, HueErr
	}
}

func vsFYCell(vsFY string) string {
	label, hue := UsagePace(vsFY)
	return tc(hue).Sprint(label)
}
