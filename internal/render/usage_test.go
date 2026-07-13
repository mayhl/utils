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
	// Mid-year (60% of the year left): only the exhaustion end grades warm — a 60%
	// surplus is 1× the even pace, perfectly on track.
	remains := []struct{ pct, fy, hue string }{
		{"60.00%", "60.00", HueOK},
		{"25.00%", "60.00", HueWarn},
		{"10.00%", "60.00", HueErr},
		{"", "60.00", HueDim},
		{"60.00%", "", HueOK}, // no banner percent → exhaustion grade only
	}
	for _, c := range remains {
		if _, hue := UsageRemain(c.pct, c.fy); hue != c.hue {
			t.Errorf("UsageRemain(%q, fy=%q) hue = %s, want %s", c.pct, c.fy, hue, c.hue)
		}
	}
	paces := []struct{ vsFY, pct, fy, hue string }{
		{"+0.3%", "60.00%", "59.70", HueOK},
		{"-5.0%", "55.00%", "60.00", HueWarn},
		{"-23.7%", "36.30%", "60.00", HueErr},
		{"", "", "", HueDim},
	}
	for _, c := range paces {
		if _, hue := UsagePace(c.vsFY, c.pct, c.fy); hue != c.hue {
			t.Errorf("UsagePace(%q, %q, fy=%q) hue = %s, want %s", c.vsFY, c.pct, c.fy, hue, c.hue)
		}
	}
}

// TestUsageUnderuse covers the use-it-or-lose-it end: the burn multiple (unspent percent
// over year-left percent) is what tightens as the year closes, so the SAME 40% surplus is
// fine in the spring and a red forfeit warning in the autumn. Both warm cells carry the
// rising glyph, telling them apart from an exhaustion warning by shape.
func TestUsageUnderuse(t *testing.T) {
	cases := []struct {
		name         string
		pct, fy, hue string
		glyph        bool
	}{
		{"comfortable mid-year", "40.00%", "80.00", HueOK, false},    // 0.5×
		{"on pace", "40.00%", "40.00", HueOK, false},                 // 1×
		{"behind, flag it", "40.00%", "20.00", HueWarn, true},        // 2×
		{"way behind, at risk", "40.00%", "10.00", HueErr, true},     // 4×
		{"tiny leftover, ignored", "20.00%", "2.00", HueWarn, false}, // 10× but ≤25% → exhaustion band owns it
		{"year over, no pace", "40.00%", "0.00", HueOK, false},       // no division by zero
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			label, hue := UsageRemain(c.pct, c.fy)
			if hue != c.hue {
				t.Errorf("UsageRemain(%q, %q) hue = %s, want %s", c.pct, c.fy, hue, c.hue)
			}
			if got := strings.Contains(label, surplusGlyph()); got != c.glyph {
				t.Errorf("UsageRemain(%q, %q) = %q, surplus glyph = %v, want %v", c.pct, c.fy, label, got, c.glyph)
			}
		})
	}
}
