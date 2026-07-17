package cli

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// TestTunnelRegistry: a tunnel survives the round trip, is findable by bare or qualified job
// id, an ambiguous id refuses rather than guesses, and forget removes it.
func TestTunnelRegistry(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	a := tunnelRec{ID: "aaaa", System: "alpha", Job: "246791.pbs01", Host: "n1", Target: "u@alpha", Sock: "/tmp/s1", LocalPort: 8888, RemotePort: 8888, Started: time.Unix(100, 0)}
	b := tunnelRec{ID: "bbbb", System: "beta", Job: "500.slurm", Host: "n2", Target: "u@beta", Sock: "/tmp/s2", LocalPort: 9000, RemotePort: 9000, Started: time.Unix(200, 0)}
	c := tunnelRec{ID: "cccc", System: "gamma", Job: "246791.other", Host: "n3", Target: "u@gamma", Sock: "/tmp/s3", LocalPort: 9100, RemotePort: 9100, Started: time.Unix(300, 0)}
	for _, r := range []tunnelRec{a, b, c} {
		if err := saveTunnel(r); err != nil {
			t.Fatal(err)
		}
	}

	// Newest first.
	got := loadTunnels()
	if len(got) != 3 || got[0].ID != c.ID {
		t.Fatalf("loadTunnels order/count wrong: %+v", got)
	}

	// The mu handle is the intended key.
	if r, err := findTunnel("aaaa"); err != nil || r.System != "alpha" {
		t.Errorf("findTunnel by id = %+v, %v", r, err)
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
	if _, err := findTunnel("aaaa"); err == nil {
		t.Error("forgotten tunnel still found")
	}
	if len(loadTunnels()) != 2 {
		t.Error("forget didn't remove exactly one")
	}
}

// TestExpired: the offline death proof. It must fire only when the job CANNOT still be
// running — a false positive here drops the record of a live job and strands it, so every
// unprovable case has to answer false.
func TestExpired(t *testing.T) {
	ago := func(d time.Duration) time.Time { return time.Now().Add(-d) }
	for _, tc := range []struct {
		name string
		rec  tunnelRec
		want bool
	}{
		{"walltime spent", tunnelRec{Walltime: "01:00:00", Running: ago(2 * time.Hour)}, true},
		{"still inside its walltime", tunnelRec{Walltime: "04:00:00", Running: ago(time.Hour)}, false},
		{"walltime unknown — the script named its own", tunnelRec{Walltime: "", Running: ago(500 * time.Hour)}, false},
		{"walltime unparseable", tunnelRec{Walltime: "UNLIMITED", Running: ago(500 * time.Hour)}, false},
		// The record predates Running: Started is the SUBMIT, and the queue wait before the job
		// ran is unknown, so it dates nothing. Old records age out via reattach, never on a guess.
		{"no running stamp", tunnelRec{Walltime: "01:00:00", Started: ago(500 * time.Hour)}, false},
	} {
		if got := expired(tc.rec); got != tc.want {
			t.Errorf("expired(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestMasterAlive: the local liveness test says dead for a socket that isn't there and for a
// path that is not a live control socket — and, the point of it, without touching the network.
func TestMasterAlive(t *testing.T) {
	if masterAlive(tunnelRec{Sock: "", Target: "u@alpha"}) {
		t.Error("a record with no socket must read dead")
	}
	if masterAlive(tunnelRec{Sock: filepath.Join(t.TempDir(), "absent"), Target: "u@alpha"}) {
		t.Error("an absent socket must read dead")
	}
	// A regular file at the socket path: present, but nothing answers `-O check` — the corpse
	// case a force-killed master leaves behind.
	corpse := filepath.Join(t.TempDir(), "mu-tun-corpse")
	if err := os.WriteFile(corpse, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if masterAlive(tunnelRec{Sock: corpse, Target: "u@alpha"}) {
		t.Error("a stale socket file must read dead")
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

	// No -l with the remote port already held locally: auto-pick WALKS up rather than refusing.
	// The tunnel command used to default -l to --port, which made this branch unreachable — a
	// busy service port hit the named-port refusal instead of moving.
	b, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	busy := b.Addr().(*net.TCPAddr).Port
	if got, err := pickLocalPort(0, busy); err != nil || got <= busy {
		t.Errorf("auto-pick over a busy remote port = %d, %v; want a free port above %d", got, err, busy)
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

// TestJobShort: the batched `ls` matches a stored id against qstat's echoed form by its
// suffix-free segment, so a PBS id that qsub returned as "N.sdb" but qstat echoes as "N.hpc1"
// still resolves. SLURM ids (no suffix) pass through unchanged.
func TestJobShort(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"1284575.sdb", "1284575"},
		{"1284575.hpc1", "1284575"},
		{"8580901", "8580901"}, // SLURM — no suffix
		{"1284[7].hpc1", "1284[7]"},
		{"", ""},
	} {
		if got := jobShort(tc.in); got != tc.want {
			t.Errorf("jobShort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestWallLeft: requested minus elapsed, read through the scheduler's own adapter. The
// two-field cases are the whole point — the same "10:00" is HH:MM on PBS and MM:SS on SLURM,
// so the answer diverges by dialect; a blank/UNLIMITED field degrades to "".
func TestWallLeft(t *testing.T) {
	for _, tc := range []struct {
		sched, req, el, want string
	}{
		// HH:MM:SS parses the same either way.
		{"slurm", "01:00:00", "00:20:00", "00:40:00"},
		{"pbs", "48:00:00", "01:30:00", "46:30:00"},
		{"slurm", "01:00:00", "02:00:00", "0s"}, // over — clamp, don't go negative
		// Two-field forms diverge: this is the bug the dialect-aware reader fixes.
		{"slurm", "10:00", "09:47", "00:00:13"},         // SLURM MM:SS: 10m − 9m47s = 13s
		{"pbs", "24:00", "06:14", "17:46:00"},           // PBS HH:MM: 24h − 6h14m
		{"slurm", "1-00:00:00", "06:14:32", "17:45:28"}, // SLURM day form
		// Degradation.
		{"slurm", "", "00:10:00", ""},        // scheduler left walltime blank
		{"pbs", "01:00:00", "UNLIMITED", ""}, // not a clock
	} {
		if got := wallLeft(queue.For(tc.sched), tc.req, tc.el); got != tc.want {
			t.Errorf("wallLeft(%s, %q, %q) = %q, want %q", tc.sched, tc.req, tc.el, got, tc.want)
		}
	}
}
