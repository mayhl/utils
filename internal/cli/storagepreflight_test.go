package cli

import (
	"testing"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

func TestMatchStorageRow(t *testing.T) {
	// Bare-root Location (Navy form): "/p/work1" is the filesystem for a deep $WORKDIR dest.
	rows := []queue.StorageInfo{
		{Location: "/p/home", DiskUsedKB: "173301600", DiskQuotaKB: "262144000"},
		{Location: "/p/work1", DiskUsedKB: "36448425932", DiskQuotaKB: "107374182400"},
		{Location: "/p/work2", DiskUsedKB: "8", DiskQuotaKB: "26843545600"},
	}
	if r, ok := matchStorageRow("/p/work1/tester/projects/sim/simulations/data", rows); !ok || r.Location != "/p/work1" {
		t.Errorf("work dest: got %q ok=%v, want /p/work1", r.Location, ok)
	}
	if r, ok := matchStorageRow("/p/home/tester/projects/sim/data/raw", rows); !ok || r.Location != "/p/home" {
		t.Errorf("home dest: got %q ok=%v, want /p/home", r.Location, ok)
	}
	// A component-boundary prefix — "/p/work1" must not match "/p/work12".
	if _, ok := matchStorageRow("/p/work12/tester/x", rows); ok {
		t.Error("work12: want no match (component boundary)")
	}
	// ERDC form: scratch $WORKDIR (/p/work) carries no quota row → no match, honest.
	erdc := []queue.StorageInfo{
		{Location: "/p/home", DiskUsedKB: "65207508", DiskQuotaKB: "104857600"},
		{Location: "/p/global", DiskUsedKB: "86686236", DiskQuotaKB: "-"},
	}
	if _, ok := matchStorageRow("/p/work/tester/projects/sim/simulations/data", erdc); ok {
		t.Error("erdc scratch: want no match (no quota row)")
	}
	if r, ok := matchStorageRow("/p/home/tester/data/raw", erdc); !ok || r.Location != "/p/home" {
		t.Errorf("erdc home: got %q ok=%v, want /p/home", r.Location, ok)
	}
}

func TestFsRoot(t *testing.T) {
	cases := map[string]string{
		"/p/work/tester/projects/sim": "/p/work",
		"/p/work1/tester":             "/p/work1",
		"/scratch":                    "/scratch",
	}
	for in, want := range cases {
		if got := fsRoot(in); got != want {
			t.Errorf("fsRoot(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseKB(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"104857600", 104857600, true},
		{"1,048,576", 1048576, true},
		{"-", 0, false},   // ERDC unlimited
		{"0", 0, false},   // Navy unlimited
		{"", 0, false},    // blank
		{"abc", 0, false}, // junk
	}
	for _, c := range cases {
		if got, ok := parseKB(c.in); got != c.want || ok != c.ok {
			t.Errorf("parseKB(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
