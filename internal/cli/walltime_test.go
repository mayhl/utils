package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// stubQueues seeds the queue cache for a machine, which is what the clamp reads — so these
// tests exercise the real path with no ssh.
func stubQueues(t *testing.T, node string, qs ...queue.QueueInfo) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeQueueCache(node, qs)
	if got := readQueueCache(node, time.Now()); len(got) != len(qs) {
		t.Fatalf("queue cache didn't take: %v", got)
	}
}

// TestResolveWalltime covers the precedence and the clamp: -t wins, --debug takes the
// queue's whole slot, the config default fills in, and NOTHING escapes the queue's maximum.
func TestResolveWalltime(t *testing.T) {
	stubQueues(
		t, "hpc1",
		queue.QueueInfo{Name: "debug", MaxWalltime: "00:30:00"},
		queue.QueueInfo{Name: "standard", MaxWalltime: "168:00:00"},
	)

	for _, tc := range []struct {
		name          string
		q, want, dflt string
		debug         bool
		expect        string
	}{
		{name: "-t wins, in shorthand", q: "standard", want: "1.5h", expect: "01:30:00"},
		{name: "-t over the max is capped", q: "debug", want: "2h", expect: "00:30:00"},
		{name: "--debug takes the whole slot", q: "debug", debug: true, expect: "00:30:00"},
		{name: "-t still beats --debug", q: "debug", want: "10m", debug: true, expect: "00:10:00"},
		{name: "config default applies", q: "standard", dflt: "1h", expect: "01:00:00"},
		{name: "config default is capped too", q: "debug", dflt: "1h", expect: "00:30:00"},
		{name: "nothing asked, nothing sent", q: "standard", expect: ""},
		{name: "unknown queue: no clamp, still normalized", q: "mystery", want: "90m", expect: "01:30:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveWalltime("hpc1", tc.q, tc.want, tc.dflt, tc.debug)
			if err != nil {
				t.Fatalf("resolveWalltime: %v", err)
			}
			if got != tc.expect {
				t.Errorf("got %q, want %q", got, tc.expect)
			}
		})
	}

	if _, err := resolveWalltime("hpc1", "standard", "90", "", false); err == nil {
		t.Error("a bare number must be refused, not read as 90 seconds")
	}
}

// TestMayInjectWalltime is the guard on the one thing mu must never do: override a walltime
// a script declared for itself. A script mu can't read counts as declaring one — the usual
// case, since the path is resolved on the cluster.
func TestMayInjectWalltime(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	silent := write("silent.sh", "#!/bin/bash\n#PBS -l select=1\necho hi\n")
	pbs := write("pbs.sh", "#!/bin/bash\n#PBS -l walltime=24:00:00\necho hi\n")
	slurm := write("slurm.sh", "#!/bin/bash\n#SBATCH -t 12:00:00\necho hi\n")
	slurmLong := write("slurm2.sh", "#!/bin/bash\n#SBATCH --time=12:00:00\necho hi\n")

	if !mayInjectWalltime("") {
		t.Error("no script at all (an interactive session) — mu has nothing to override")
	}
	if !mayInjectWalltime(silent) {
		t.Error("a script that declares no walltime may be given one")
	}
	for _, s := range []string{pbs, slurm, slurmLong} {
		if mayInjectWalltime(s) {
			t.Errorf("%s declares a walltime — mu must not override it", filepath.Base(s))
		}
	}
	if mayInjectWalltime(filepath.Join(dir, "lives-on-the-cluster.sh")) {
		t.Error("an unreadable script must be assumed to declare one — overriding what you can't see is the whole hazard")
	}
}
