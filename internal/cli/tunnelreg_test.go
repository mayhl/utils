package cli

import (
	"net"
	"testing"
	"time"
)

// TestTunnelRegistry: a tunnel survives the round trip, is findable by bare or qualified job
// id, an ambiguous id refuses rather than guesses, and forget removes it.
func TestTunnelRegistry(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	a := tunnelRec{System: "alpha", Job: "246791.pbs01", Host: "n1", Target: "u@alpha", Sock: "/tmp/s1", LocalPort: 8888, RemotePort: 8888, Started: time.Unix(100, 0)}
	b := tunnelRec{System: "beta", Job: "500.slurm", Host: "n2", Target: "u@beta", Sock: "/tmp/s2", LocalPort: 9000, RemotePort: 9000, Started: time.Unix(200, 0)}
	c := tunnelRec{System: "gamma", Job: "246791.other", Host: "n3", Target: "u@gamma", Sock: "/tmp/s3", LocalPort: 9100, RemotePort: 9100, Started: time.Unix(300, 0)}
	for _, r := range []tunnelRec{a, b, c} {
		if err := saveTunnel(r); err != nil {
			t.Fatal(err)
		}
	}

	// Newest first.
	got := loadTunnels()
	if len(got) != 3 || got[0].Job != c.Job {
		t.Fatalf("loadTunnels order/count wrong: %+v", got)
	}

	// A bare number that prefixes exactly one job id resolves (beta's is unambiguous).
	if r, err := findTunnel("500"); err != nil || r.System != "beta" {
		t.Errorf("findTunnel(500) = %+v, %v", r, err)
	}
	// 246791 is on two systems → refuse.
	if _, err := findTunnel("246791"); err == nil {
		t.Error("an ambiguous job id must not resolve to one tunnel")
	}
	// Qualifying it disambiguates.
	if r, err := findTunnel("alpha/246791.pbs01"); err != nil || r.System != "alpha" {
		t.Errorf("qualified find = %+v, %v", r, err)
	}

	forgetTunnel(a)
	if _, err := findTunnel("alpha/246791.pbs01"); err == nil {
		t.Error("forgotten tunnel still found")
	}
	if len(loadTunnels()) != 2 {
		t.Error("forget didn't remove exactly one")
	}
}

// TestPickLocalPort: a named port is honoured or refused (never silently moved), an absent
// one auto-picks starting at the remote port, and privileged ports are refused.
func TestPickLocalPort(t *testing.T) {
	// A free named port comes back as-is.
	free := freePort(t)
	if got, err := pickLocalPort(free, 8888); err != nil || got != free {
		t.Errorf("named free port = %d, %v; want %d", got, err, free)
	}

	// A named port that's taken is refused — NOT bumped.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	taken := l.Addr().(*net.TCPAddr).Port
	if _, err := pickLocalPort(taken, 8888); err == nil {
		t.Errorf("a named busy port (%d) must be refused, not moved", taken)
	}

	// Privileged named ports are refused before any bind.
	if _, err := pickLocalPort(80, 8888); err == nil {
		t.Error("a privileged port must be refused")
	}

	// No -l: auto-pick starts AT the remote port when it's free, so the URL is predictable.
	remote := freePort(t)
	if got, err := pickLocalPort(0, remote); err != nil || got != remote {
		t.Errorf("auto-pick = %d, %v; want it to start at the remote port %d", got, err, remote)
	}
}

// freePort returns a port that is free right now (the listener is closed before returning,
// so pickLocalPort can bind it — a small TOCTOU window the test accepts).
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return p
}

// TestWallLeft: requested minus elapsed, degrading to "" when a field is unparseable.
func TestWallLeft(t *testing.T) {
	for _, tc := range []struct{ req, el, want string }{
		{"01:00:00", "00:20:00", "00:40:00"},
		{"48:00:00", "01:30:00", "46:30:00"},
		{"01:00:00", "02:00:00", "0s"}, // over — clamp, don't go negative
		{"", "00:10:00", ""},           // scheduler left walltime blank
		{"01:00:00", "UNLIMITED", ""},  // a form ParseWalltime doesn't read
	} {
		if got := wallLeft(tc.req, tc.el); got != tc.want {
			t.Errorf("wallLeft(%q,%q) = %q, want %q", tc.req, tc.el, got, tc.want)
		}
	}
}
