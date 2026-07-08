package cli

import (
	"testing"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// TestExecQueues locks the default `mu hpc queues` filter: only submittable Exe queues
// survive. Some systems put a routing "Rou" or a bare integer in the Typ column — those
// are admin/routing queues a user never submits to, so they're dropped (visible under -a).
func TestExecQueues(t *testing.T) {
	in := []queue.QueueInfo{
		{Name: "standard", Type: "Exe"},
		{Name: "route", Type: "Rou"},
		{Name: "numbered", Type: "2"}, // integer Typ seen on some systems — not submittable
		{Name: "lower", Type: "exe"},  // case-insensitive
		{Name: "blank", Type: ""},     // unreported Type → not Exe
	}
	got := execQueues(in)
	if len(got) != 2 {
		t.Fatalf("want 2 Exe queues, got %d: %+v", len(got), got)
	}
	if got[0].Name != "standard" || got[1].Name != "lower" {
		t.Errorf("kept the wrong queues: %+v", got)
	}
}

// TestUpQueues locks the last default-view filter: only known-down queues (E=N or R=N) are
// dropped and counted; up queues and ones with blank/"-" flags (state not reported) survive.
func TestUpQueues(t *testing.T) {
	in := []queue.QueueInfo{
		{Name: "up", Enabled: "Y", Running: "Y"},
		{Name: "stopped", Enabled: "Y", Running: "N"},
		{Name: "disabled", Enabled: "N", Running: "N"},
		{Name: "blank", Enabled: "", Running: ""}, // not reported → kept (can't confirm down)
		{Name: "dash", Enabled: "-", Running: "-"},
	}
	up, down := upQueues(in)
	if down != 2 {
		t.Fatalf("want 2 down, got %d: up=%+v", down, up)
	}
	if len(up) != 3 || up[0].Name != "up" || up[1].Name != "blank" || up[2].Name != "dash" {
		t.Errorf("kept the wrong queues: %+v", up)
	}
}

// TestMaxNodesFrom locks the MaxNodes ceil math + the blank cases (unset cores/node, or a
// non-numeric MaxCores like an unlimited "--").
func TestMaxNodesFrom(t *testing.T) {
	cases := []struct {
		maxCores string
		cpn      int
		want     string
	}{
		{"4096", 128, "32"}, // exact
		{"4097", 128, "33"}, // ceil rounds up
		{"100", 128, "1"},   // fits in one node
		{"256", 0, ""},      // cores/node unset → blank
		{"--", 128, ""},     // unlimited/non-numeric MaxCores → blank
		{"", 128, ""},
	}
	for _, c := range cases {
		if got := maxNodesFrom(c.maxCores, c.cpn); got != c.want {
			t.Errorf("maxNodesFrom(%q, %d) = %q, want %q", c.maxCores, c.cpn, got, c.want)
		}
	}
}
