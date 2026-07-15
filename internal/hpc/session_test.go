package hpc

import (
	"strings"
	"testing"
)

// TestRsyncTransport pins the -e string rsync rides the session's master with: it must name
// the ssh binary, point at the control socket, and force ControlMaster=no so rsync's ssh is a
// CLIENT of the existing master, never a second master.
func TestRsyncTransport(t *testing.T) {
	s := &Session{bin: "ssh", sock: "/tmp/mu-mux-42", target: "u@host"}
	tr := s.RsyncTransport()
	for _, want := range []string{"ssh", "-S /tmp/mu-mux-42", "ControlMaster=no"} {
		if !strings.Contains(tr, want) {
			t.Errorf("RsyncTransport() = %q, missing %q", tr, want)
		}
	}
}

// TestSessionSock: the socket path is the handle the tunnel registry stores.
func TestSessionSock(t *testing.T) {
	s := &Session{sock: "/tmp/mu-tun-abc"}
	if got := s.Sock(); got != "/tmp/mu-tun-abc" {
		t.Errorf("Sock() = %q", got)
	}
}
