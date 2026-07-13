package render

import "testing"

// TestPlanBar covers the bar's width-fitting: roomy terminals keep the full 30-col bar and
// spend the surplus on the label, and narrow ones shrink the BAR (to its floor) instead of
// letting the line wrap — a wrapped in-place bar smears across the scrollback.
func TestPlanBar(t *testing.T) {
	// planBar keeps the last column empty (a written final column arms an auto-wrap on
	// some terminals), so the usable width is tw-1 throughout.
	cases := []struct {
		name           string
		tw             int
		wantBar, minLb int
	}{
		{"unknown width assumes 80", 0, 30, minLabelWidth},
		{"wide keeps the full bar", 200, barWidth, minLabelWidth},
		{"exactly enough for both", 1 + barFixedCols + barWidth + minLabelWidth, barWidth, minLabelWidth},
		{"narrow shrinks the bar", 60, 59 - barFixedCols - minLabelWidth, minLabelWidth},
		{"very narrow floors the bar", 40, minBarWidth, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bar, label := planBar(c.tw)
			if bar != c.wantBar {
				t.Errorf("planBar(%d) bar = %d, want %d", c.tw, bar, c.wantBar)
			}
			if bar < minBarWidth || bar > barWidth {
				t.Errorf("planBar(%d) bar = %d, outside [%d,%d]", c.tw, bar, minBarWidth, barWidth)
			}
			if label < c.minLb {
				t.Errorf("planBar(%d) label = %d, want ≥ %d", c.tw, label, c.minLb)
			}
			// The whole line must fit with the last column to spare.
			tw := c.tw
			if tw <= 0 {
				tw = 80
			}
			if label > 0 && barFixedCols+bar+label > tw-1 {
				t.Errorf("planBar(%d) line = %d cols, overflows the terminal", c.tw, barFixedCols+bar+label)
			}
		})
	}
}
