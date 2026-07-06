package hpc

import "testing"

// TestFirstLine: the terse error summary folded into a bounded remote-exec failure
// is the first non-blank line, trimmed.
func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"\n\n  \nhello\nworld": "hello",
		"  trim me  \n":        "trim me",
		"only":                 "only",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
