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

// TestLocalExec: the on-cluster exec seam returns a command's stdout on success and a
// terse exit-code reason on failure — separate buffers keep stdout clean and preserve
// the real exit code (no shell pipe).
func TestLocalExec(t *testing.T) {
	out, err := LocalExec("echo hi")
	if err != nil {
		t.Fatalf("LocalExec(echo) err = %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Errorf("LocalExec(echo) out = %q, want %q", out, "hi")
	}
	// a 127 exit maps through exitText to the not-found reason, not a raw wait-status
	if _, err := LocalExec("exit 127"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("LocalExec(exit 127) err = %v, want it to contain %q", err, "not found")
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
