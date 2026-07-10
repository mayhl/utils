package cli

import "testing"

func TestKBHuman(t *testing.T) {
	cases := map[string]string{
		"52428800":    "50.0GB",
		"21474836480": "20.0TB",
		"0":           "0B",
		"":            "",
		"--":          "",
	}
	for in, want := range cases {
		if got := kbHuman(in); got != want {
			t.Errorf("kbHuman(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCountHuman(t *testing.T) {
	cases := map[string]string{
		"4000000": "4.0M",
		"41250":   "41.2k",
		"9999":    "9999",
		"0":       "0",
		"":        "",
	}
	for in, want := range cases {
		if got := countHuman(in); got != want {
			t.Errorf("countHuman(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUsedPct(t *testing.T) {
	cases := []struct{ used, quota, want string }{
		{"52428800", "104857600", "50"},
		{"96", "100", "96"},
		{"1", "3", "33"},
		{"2", "3", "67"}, // rounds, not truncates
		{"10", "0", ""},  // zero quota = unlimited, no percent
		{"--", "100", ""},
		{"10", "", ""},
	}
	for _, c := range cases {
		if got := usedPct(c.used, c.quota); got != c.want {
			t.Errorf("usedPct(%q, %q) = %q, want %q", c.used, c.quota, got, c.want)
		}
	}
}
