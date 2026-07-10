package cli

import (
	"testing"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// TestCollatedUsageRows checks the grouped collate layout: repeated subprojects blanked
// after the group's first row, and a multi-system group summed into a bold total row
// (Remain% recomputed from the sums, pace against the shared FY percent). A single-system
// group gets no total.
func TestCollatedUsageRows(t *testing.T) {
	abc := queue.UsageInfo{
		Subproject: "ABC123DEF", Allocated: "1000000", Used: "400000",
		Remaining: "600000", PctRemain: "60.00%", Background: "0", FYLeft: "22.67",
	}
	abc2 := abc
	abc2.System, abc2.Allocated, abc2.Used, abc2.Remaining, abc2.PctRemain, abc2.Background =
		"sys2", "500000", "450000", "50000", "10.00%", "12345"
	abc.System = "sys1"
	solo := queue.UsageInfo{
		System: "sys1", Subproject: "QRS456JKL", Allocated: "200000", Used: "140000",
		Remaining: "60000", PctRemain: "30.00%", Background: "0", FYLeft: "22.67",
	}

	rows := collatedUsageRows([]queue.UsageInfo{abc, abc2, solo})
	if len(rows) != 4 { // 2 ABC + total + 1 QRS
		t.Fatalf("want 4 rows, got %d: %+v", len(rows), rows)
	}
	if rows[1].Subproject != "" {
		t.Errorf("group continuation should blank the subproject: %+v", rows[1])
	}
	tot := rows[2]
	if !tot.Total || tot.System != "total" {
		t.Fatalf("row2 should be the total: %+v", tot)
	}
	// 1.5M allocated, 650k remaining → 43.33%, pace 43.33−22.67 = +20.7
	if tot.Allocated != "1.5M" || tot.Remaining != "650.0k" || tot.RemainPct != "43.33%" || tot.VsFY != "+20.7%" {
		t.Errorf("total row mismatch: %+v", tot)
	}
	if rows[3].Subproject != "QRS456JKL" || rows[3].Total {
		t.Errorf("single-system group must keep its name and get no total: %+v", rows[3])
	}
}

func TestVsFY(t *testing.T) {
	cases := []struct{ remain, fy, want string }{
		{"60.00%", "33.70", "+26.3%"},
		{"10.00%", "33.70", "-23.7%"},
		{"33.70%", "33.70", "+0.0%"}, // on pace counts as under budget
		{"60.00%", "", ""},           // no banner percent → column degrades away
		{"--", "33.70", ""},
	}
	for _, c := range cases {
		if got := vsFY(c.remain, c.fy); got != c.want {
			t.Errorf("vsFY(%q, %q) = %q, want %q", c.remain, c.fy, got, c.want)
		}
	}
}
