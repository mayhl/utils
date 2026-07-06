package hpc

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

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

func TestExitText(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{255, "ticket"}, // both 255 branches (no-ticket / auth-or-host) name the ticket
		{127, "not found"},
		{126, "permission denied"},
	}
	for _, c := range cases {
		err := exec.Command("sh", "-c", "exit "+strconv.Itoa(c.code)).Run()
		if got := exitText(err); !strings.Contains(got, c.want) {
			t.Errorf("exit %d → %q, want it to contain %q", c.code, got, c.want)
		}
	}
	// a non-ExitError falls through to its message
	if got := exitText(errors.New("boom")); got != "boom" {
		t.Errorf("plain error: %q", got)
	}
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"user@login.example": "login.example",
		"hpc1":               "hpc1",
		"a@b@c":              "c", // strips through the last @
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}
