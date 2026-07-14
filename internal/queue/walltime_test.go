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
