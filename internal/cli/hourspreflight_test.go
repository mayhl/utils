package cli

import (
	"testing"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

func TestFmtHours(t *testing.T) {
	cases := map[float64]string{
		500:     "500",
		6144:    "6,144",
		480000:  "480,000",
		1234567: "1,234,567",
		6143.6:  "6,144", // rounds
	}
	for in, want := range cases {
		if got := fmtHours(in); got != want {
			t.Errorf("fmtHours(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestParseHoursNum(t *testing.T) {
	if v, ok := parseHoursNum("1,985"); !ok || v != 1985 {
		t.Errorf("parseHoursNum(1,985) = %v,%v", v, ok)
	}
	if v, ok := parseHoursNum(" 8760 "); !ok || v != 8760 {
		t.Errorf("parseHoursNum(8760) = %v,%v", v, ok)
	}
	for _, in := range []string{"", "  ", "n/a", "--"} {
		if _, ok := parseHoursNum(in); ok {
			t.Errorf("parseHoursNum(%q) ok, want reject", in)
		}
	}
}

func TestMatchUsage(t *testing.T) {
	rows := []queue.UsageInfo{
		{Subproject: "OTHERV001", Allocated: "1000", Remaining: "500"},
		{Subproject: "PROJV00001", Allocated: "10,000", Remaining: "1,985"},
	}
	// Case-insensitive match on the allocation code, commas stripped.
	if a, r, ok := matchUsage(rows, "projv00001"); !ok || a != 10000 || r != 1985 {
		t.Errorf("match = %v,%v,%v want 10000,1985,true", a, r, ok)
	}
	if _, _, ok := matchUsage(rows, "no-such-code"); ok {
		t.Error("unmatched account: want ok=false")
	}
}

func TestAllocationHoursOfflineCache(t *testing.T) {
	// Pin the cache under a temp dir so the test never touches the real one.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeUsageCache("hpc1", []queue.UsageInfo{
		{Subproject: "ACCT", Allocated: "10000", Remaining: "1000"},
	})

	// mayFetch=false (dry-run / no ticket): reads the cache, never fetches. Even at the
	// near-edge (est > 90% of remaining) it degrades to the cached number rather than
	// forcing a live call it isn't allowed to make.
	a, r, stamp, ok := allocationHours("hpc1", "ACCT", 950, false)
	if !ok || a != 10000 || r != 1000 {
		t.Fatalf("cached: a=%v r=%v ok=%v", a, r, ok)
	}
	if stamp == "" {
		t.Error("expected a staleness stamp")
	}

	// No account → nothing to match against → no percentages.
	if _, _, _, ok := allocationHours("hpc1", "", 950, false); ok {
		t.Error("empty account: want ok=false")
	}
	// A machine with no cache and no fetch permitted → no data.
	if _, _, _, ok := allocationHours("no-cache-node", "ACCT", 950, false); ok {
		t.Error("uncached node offline: want ok=false")
	}
}
