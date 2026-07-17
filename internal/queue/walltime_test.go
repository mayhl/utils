package queue

import "testing"

// TestParseWalltime locks the shorthand, and above all what it REFUSES: a bare number, which
// PBS would read as seconds.
func TestParseWalltime(t *testing.T) {
	ok := map[string]int{
		"00:10:00":  600,
		"168:00:00": 168 * 3600,
		"10m":       600,
		"1h":        3600,
		"1.5h":      5400,
		"1h30m":     5400,
		"2d":        2 * 86400,
		" 45M ":     2700, // trimmed and case-folded
		"90s":       90,
	}
	for in, want := range ok {
		if got, valid := ParseWalltime(in); !valid || got != want {
			t.Errorf("ParseWalltime(%q) = %d,%v; want %d", in, got, valid, want)
		}
	}
	for _, bad := range []string{"", "90", "1.5", "1h junk", "junk1h", "1x", "1:2:3:4", "0:60:00", "-1h"} {
		if got, valid := ParseWalltime(bad); valid {
			t.Errorf("ParseWalltime(%q) = %d, accepted — want refused", bad, got)
		}
	}
}

// TestNormalizeWalltime: what leaves mu is always canonical, and a blank stays a blank (=
// send no walltime at all, letting the script or the scheduler decide).
func TestNormalizeWalltime(t *testing.T) {
	for in, want := range map[string]string{
		"1.5h":     "01:30:00",
		"10m":      "00:10:00",
		"2d":       "48:00:00",
		"01:30:00": "01:30:00",
		"":         "",
		"  ":       "",
	} {
		if got, ok := NormalizeWalltime(in); !ok || got != want {
			t.Errorf("NormalizeWalltime(%q) = %q,%v; want %q", in, got, ok, want)
		}
	}
	if _, ok := NormalizeWalltime("90"); ok {
		t.Error("a bare number must not normalize")
	}
}

// TestParseDuration covers reading a scheduler's OWN time cells, where the same two-field
// string means different things per dialect — PBS HH:MM vs SLURM MM:SS — and only the day
// form and non-clock values differ further.
func TestParseDuration(t *testing.T) {
	pbs, slurm := For("pbs"), For("slurm")
	for _, tc := range []struct {
		a       Adapter
		in      string
		wantSec int
		wantOK  bool
	}{
		{pbs, "24:00", 24 * 3600, true},              // PBS Req'd Time: HH:MM
		{pbs, "06:14", 6*3600 + 14*60, true},         // PBS Elapsed short: HH:MM
		{pbs, "06:14:52", 6*3600 + 14*60 + 52, true}, // PBS HH:MM:SS
		{slurm, "10:00", 10 * 60, true},              // SLURM MM:SS — NOT ten hours
		{slurm, "9:47", 9*60 + 47, true},             // SLURM MM:SS, single-digit
		{slurm, "01:30:00", 5400, true},              // SLURM HH:MM:SS
		{slurm, "1-00:00:00", 86400, true},           // SLURM D-HH:MM:SS
		{slurm, "2-06:00:00", 2*86400 + 6*3600, true},
		{slurm, "UNLIMITED", 0, false}, // partition cap with no limit
		{slurm, "N/A", 0, false},       // pending job, no elapsed yet
		{pbs, "", 0, false},            // blank cell
		{slurm, "1-", 0, false},        // day prefix, no clock
		{pbs, "1:2:3:4", 0, false},     // too many fields
	} {
		sec, ok := tc.a.ParseDuration(tc.in)
		if ok != tc.wantOK || (ok && sec != tc.wantSec) {
			t.Errorf("%s ParseDuration(%q) = %d,%v; want %d,%v",
				tc.a.Name(), tc.in, sec, ok, tc.wantSec, tc.wantOK)
		}
	}
}
