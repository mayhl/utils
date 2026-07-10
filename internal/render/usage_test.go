package render

import (
	"strings"
	"testing"
)

func usageHeaders(rows []UsageRow) string {
	cols := planUsageCols(rows)
	h := make([]string, len(cols))
	for i, c := range cols {
		h[i] = c.header
	}
	return strings.Join(h, " ")
}

func TestPlanUsageCols(t *testing.T) {
	full := UsageRow{
		Subproject: "ABC123DEF", Allocated: "1000000", Used: "400000",
		Remaining: "600000", RemainPct: "60.00%", Background: "12345", VsFY: "+26.3%",
	}
	bare := full
	bare.Background, bare.VsFY = "0", ""
	collated := full
	collated.System = "hpc1"

	cases := []struct {
		name string
		rows []UsageRow
		want string
	}{
		{"full", []UsageRow{full}, "Subproject Allocated Used Remaining Remain% Background vs FY"},
		{"no background, no FY", []UsageRow{bare}, "Subproject Allocated Used Remaining Remain%"},
		// Subproject stays the leading (grouping) column; System slots second.
		{"collate adds System", []UsageRow{collated}, "Subproject System Allocated Used Remaining Remain% Background vs FY"},
	}
	for _, c := range cases {
		if got := usageHeaders(c.rows); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
	}
}

func TestUsageGrades(t *testing.T) {
	remains := []struct{ in, hue string }{
		{"60.00%", HueOK}, {"25.00%", HueWarn}, {"10.00%", HueErr}, {"", HueDim},
	}
	for _, c := range remains {
		if _, hue := UsageRemain(c.in); hue != c.hue {
			t.Errorf("UsageRemain(%q) hue = %s, want %s", c.in, hue, c.hue)
		}
	}
	paces := []struct{ in, hue string }{
		{"+26.3%", HueOK}, {"+0.0%", HueOK}, {"-5.0%", HueWarn}, {"-23.7%", HueErr}, {"", HueDim},
	}
	for _, c := range paces {
		if _, hue := UsagePace(c.in); hue != c.hue {
			t.Errorf("UsagePace(%q) hue = %s, want %s", c.in, hue, c.hue)
		}
	}
}
