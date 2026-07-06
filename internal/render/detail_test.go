package render

import (
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
)

// TestLongTime: ISO stamps expand to the verbose card form (day month year, no seconds);
// SLURM "not yet" sentinels blank out; a PBS human string passes through.
func TestLongTime(t *testing.T) {
	cases := map[string]string{
		"2026-07-06T00:00:00":      "6 Jul 2026 00:00",
		"2026-12-15T14:30:59":      "15 Dec 2026 14:30",
		"Unknown":                  "",
		"N/A":                      "",
		"None":                     "",
		"":                         "",
		"Sat Jul  5 17:40:00 2026": "Sat Jul  5 17:40:00 2026", // PBS human string verbatim
	}
	for in, want := range cases {
		if got := longTime(in); got != want {
			t.Errorf("longTime(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExitColors: a clean exit (0 / 0:0 / empty) is green, anything else red.
func TestExitColors(t *testing.T) {
	green := []string{"", "0", "0:0"}
	red := []string{"1", "1:0", "0:9", "127"}
	for _, s := range green {
		if got := exitColors(s); len(got) == 0 || got[0] != text.FgGreen {
			t.Errorf("exitColors(%q) = %v, want green", s, got)
		}
	}
	for _, s := range red {
		if got := exitColors(s); len(got) == 0 || got[0] != text.FgRed {
			t.Errorf("exitColors(%q) = %v, want red", s, got)
		}
	}
}
