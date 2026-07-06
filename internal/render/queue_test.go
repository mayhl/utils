package render

import "testing"

// TestDurSecs: right-anchored parse — SLURM forms exact, PBS "HH:MM" 60× low but
// self-consistent, non-numeric limits rejected.
func TestDurSecs(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"06:14:52", 6*3600 + 14*60 + 52, true}, // HH:MM:SS
		{"5:03", 5*60 + 3, true},                // MM:SS
		{"1-00:00:00", 86400, true},             // SLURM day form
		{"2-12:00:00", 2*86400 + 12*3600, true},
		{"24:00", 24 * 60, true}, // PBS HH:MM read as MM:SS (60× low, ratio-safe)
		{"UNLIMITED", 0, false},
		{"--", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := durSecs(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("durSecs(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestWalltimeLevel: only running jobs graded; ratio scale-invariant across formats;
// warn ≥75%, error ≥90%; unparseable/zero limit → no color.
func TestWalltimeLevel(t *testing.T) {
	cases := []struct {
		state, elapsed, wall, want string
	}{
		{"running", "12:00", "24:00", ""},      // 50% → none
		{"running", "18:00", "24:00", "warn"},  // 75%
		{"running", "22:00", "24:00", "error"}, // ~92%
		{"running", "24:00", "24:00", "error"}, // 100%
		{"queued", "00:00", "24:00", ""},       // not running
		{"exiting", "23:59", "24:00", ""},      // not running
		{"running", "5:00", "2-00:00:00", ""},  // mixed formats, tiny ratio
		{"running", "22:00", "UNLIMITED", ""},  // unparseable limit
		{"running", "--", "24:00", ""},         // no elapsed
	}
	for _, c := range cases {
		if got := walltimeLevel(c.state, c.elapsed, c.wall); got != c.want {
			t.Errorf("walltimeLevel(%q,%q,%q) = %q, want %q", c.state, c.elapsed, c.wall, got, c.want)
		}
	}
}

// TestStartCell: ISO stamps collapse to "MM-DD HH:MM"; non-ISO / empty pass through
// dash().
func TestStartCell(t *testing.T) {
	cases := map[string]string{
		"2026-07-06T14:30:00": "07-06 14:30",
		"2026-12-31T00:05:59": "12-31 00:05",
		"N/A":                 "N/A",
		"Unknown":             "Unknown",
		"":                    "--",
		"--":                  "--",
	}
	for in, want := range cases {
		if got := startCell(in); got != want {
			t.Errorf("startCell(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAnyCluster: the Cluster column shows only when a row carries a cluster tag
// (a cross-cluster collate), and stays hidden for single-cluster views.
func TestAnyCluster(t *testing.T) {
	if anyCluster([]JobRow{{ID: "1"}, {ID: "2"}}) {
		t.Error("no cluster tags → want false")
	}
	if !anyCluster([]JobRow{{ID: "1"}, {ID: "2", Cluster: "alpha"}}) {
		t.Error("a cluster tag present → want true")
	}
}
