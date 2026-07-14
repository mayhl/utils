package cli

import (
	"bytes"
	"strings"
	"testing"
)

// feed writes s to the view in awkward chunks — the pty delivers bytes, not lines, and a
// filter that only works on whole-line writes would pass a test and hang a prompt.
func feed(a *allocView, s string, chunk int) {
	for i := 0; i < len(s); i += chunk {
		end := min(i+chunk, len(s))
		_, _ = a.Write([]byte(s[i:end]))
	}
}

// TestAllocViewSession is the real transcript: the login preamble is dropped, salloc's six
// lines collapse to three house lines, and the shell that follows passes through untouched.
func TestAllocViewSession(t *testing.T) {
	var out bytes.Buffer
	a := newAllocView(&out)
	feed(a, "Last login: Mon Jul 14\n"+
		"*** CONSENT TO MONITORING ***\n"+
		"dbus-update-activation-environment: setting DISPLAY\n"+
		"salloc: Pending job allocation 8574510\n"+
		"salloc: job 8574510 queued and waiting for resources\n"+
		"salloc: job 8574510 has been allocated resources\n"+
		"salloc: Granted job allocation 8574510\n"+
		"salloc: Waiting for resource configuration\n"+
		"salloc: Nodes n1409 are ready for job\n"+
		"user@n1409:~$ ", 7)
	a.flush()

	got := out.String()
	// The preamble is gone — banner, MOTD and the dbus line all preceded salloc.
	for _, gone := range []string{"CONSENT", "Last login", "dbus-update"} {
		if strings.Contains(got, gone) {
			t.Errorf("preamble leaked: %q still in output:\n%s", gone, got)
		}
	}
	// The shell's prompt has NO trailing newline: it must not be held back.
	if !strings.Contains(got, "user@n1409:~$ ") {
		t.Errorf("the shell prompt never arrived:\n%s", got)
	}
	if a.jobID != "8574510" {
		t.Errorf("job id = %q, want 8574510", a.jobID)
	}
	// salloc's own lines are re-rendered, not passed through verbatim.
	if strings.Contains(got, "salloc: Granted") {
		t.Errorf("raw salloc chatter passed through:\n%s", got)
	}
}

// TestAllocViewNeverAllocates is the one thing this filter must never do: eat the reason a
// session failed. If salloc never speaks, everything held is the whole story.
func TestAllocViewNeverAllocates(t *testing.T) {
	var out bytes.Buffer
	a := newAllocView(&out)
	feed(a, "Permission denied (gssapi-keyex,publickey).\n", 5)
	a.flush()
	if !strings.Contains(out.String(), "Permission denied") {
		t.Errorf("a session that died before salloc must still print why, got %q", out.String())
	}
}

// TestAllocViewUnknownSallocLine: an unrecognized salloc message is surfaced, never dropped
// — mu's map of what salloc says is not the same as what salloc CAN say.
func TestAllocViewUnknownSallocLine(t *testing.T) {
	var out bytes.Buffer
	a := newAllocView(&out)
	feed(a, "salloc: Nodes n1409 are ready for job\nhello from the shell\n", 9)
	a.flush()
	if !strings.Contains(out.String(), "hello from the shell") {
		t.Errorf("the shell's own output was swallowed: %q", out.String())
	}
}
