package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// TestStagedRegistry: a pushed-script record survives the round trip, loads newest-first, and
// forget removes exactly one.
func TestStagedRegistry(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	a := stagedRec{ID: "aaaa", Node: "hpc1", System: "hpc1", Job: "100", Started: time.Unix(100, 0)}
	b := stagedRec{ID: "bbbb", Node: "", System: "node-a", Job: "246791.pbs01", Started: time.Unix(200, 0)}
	for _, r := range []stagedRec{a, b} {
		if err := saveStaged(r); err != nil {
			t.Fatal(err)
		}
	}

	got := loadStaged()
	if len(got) != 2 || got[0].ID != b.ID {
		t.Fatalf("loadStaged order/count wrong: %+v", got)
	}

	forgetStaged(a)
	if g := loadStaged(); len(g) != 1 || g[0].ID != b.ID {
		t.Errorf("forget didn't remove exactly one: %+v", g)
	}
}

// TestSweepStagedOn: only records on the swept node whose job has left the queue are reaped;
// a live job keeps its script, an off-node record is untouched, and a listing failure reaps
// nothing (an unanswered query is not proof a job ended).
func TestSweepStagedOn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// live=101 (a PBS id whose listing echoes a different host suffix than was stored), 103 gone.
	seed := []stagedRec{
		{ID: "live", Node: "hpc1", System: "hpc1", Job: "101.sdb"},
		{ID: "dead", Node: "hpc1", System: "hpc1", Job: "103"},
		{ID: "other", Node: "node-a", System: "node-a", Job: "500"},
	}
	save := func() {
		for _, r := range seed {
			if err := saveStaged(r); err != nil {
				t.Fatal(err)
			}
		}
	}
	save()

	snapshot := func() ([]queue.Job, error) {
		return []queue.Job{{ID: "101.hpc1", ShortID: "101"}, {ID: "999", ShortID: "999"}}, nil
	}
	var removed []string
	capture := func(c string) (string, error) {
		if strings.HasPrefix(c, "rm -f") {
			removed = append(removed, c)
		}
		return "", nil
	}

	if n := sweepStagedOn("hpc1", "hpc1", snapshot, capture, false); n != 1 {
		t.Fatalf("reaped %d, want 1 (only the dead hpc1 job)", n)
	}
	if len(removed) != 1 || !strings.Contains(removed[0], "dead.sh") {
		t.Errorf("removed the wrong file(s): %v", removed)
	}
	// The live record and the off-node record must remain.
	left := map[string]bool{}
	for _, r := range loadStaged() {
		left[r.ID] = true
	}
	if !left["live"] || !left["other"] || left["dead"] {
		t.Errorf("registry after sweep = %v; want live+other kept, dead gone", left)
	}

	// A listing failure reaps nothing — reset and prove it.
	forgetStaged(stagedRec{ID: "live"})
	forgetStaged(stagedRec{ID: "other"})
	save()
	removed = nil
	failing := func() ([]queue.Job, error) { return nil, fmt.Errorf("ssh down") }
	if n := sweepStagedOn("hpc1", "hpc1", failing, capture, false); n != 0 || len(removed) != 0 {
		t.Errorf("a failed listing must reap nothing: n=%d removed=%v", n, removed)
	}
}
