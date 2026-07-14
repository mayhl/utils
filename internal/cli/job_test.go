package cli

import (
	"strings"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// TestClassQueues locks the live class-flag fallback filter: up submittable queues whose
// node class (name heuristic; no config here) matches, input order preserved. Down and
// routing queues never resolve a class flag even when the name matches.
func TestClassQueues(t *testing.T) {
	qs := []queue.QueueInfo{
		{Name: "standard", Type: "Exe", Enabled: "Y", Running: "Y"},
		{Name: "gpu_short", Type: "Exe", Enabled: "Y", Running: "Y"},
		{Name: "gpu_long", Type: "Exe", Enabled: "Y", Running: "Y"},
		{Name: "viz", Type: "Exe", Enabled: "Y", Running: "Y"},
		{Name: "bigmem", Type: "Exe", Enabled: "N", Running: "Y"},    // down → dropped
		{Name: "gpu_route", Type: "Rou", Enabled: "Y", Running: "Y"}, // routing → dropped
		{Name: "transfer", Type: "Exe", Enabled: "Y", Running: "Y"},
	}
	cases := []struct {
		class string
		want  string // comma-joined expected names
	}{
		{"GPU", "gpu_short,gpu_long"},
		{"VIS", "viz"},
		{"BigMem", ""}, // only candidate is down
		{"Xfer", "transfer"},
		{"CPU", "standard"},
	}
	for _, c := range cases {
		got := strings.Join(classQueues("testcluster", c.class, qs), ",")
		if got != c.want {
			t.Errorf("classQueues(%s) = %q, want %q", c.class, got, c.want)
		}
	}
}

// TestResolveLiteralQueue: the conventional name for a purpose tier is a CONVENTION, and a
// real SLURM machine turned out not to have a partition called "debug" at all. `--debug` sent
// it anyway, and salloc failed with "invalid partition specified" — naming nothing useful.
func TestResolveLiteralQueue(t *testing.T) {
	exe := func(name string) queue.QueueInfo {
		return queue.QueueInfo{Name: name, Type: "Execution", Enabled: "Y", Running: "Y"}
	}

	// The machine really has it → use it.
	got, err := resolveLiteralQueue("hpc1", "debug", "debug", []queue.QueueInfo{exe("standard"), exe("debug")})
	if err != nil || got != "debug" {
		t.Errorf("literal present: got %q, %v", got, err)
	}
	// It doesn't, but one queue carries the word → take that one.
	got, err = resolveLiteralQueue("hpc1", "debug", "debug", []queue.QueueInfo{exe("standard"), exe("cpu_debug")})
	if err != nil || got != "cpu_debug" {
		t.Errorf("single match: got %q, %v", got, err)
	}
	// Nothing resembles it → refuse, and say what the machine DOES have.
	_, err = resolveLiteralQueue("hpc1", "debug", "debug", []queue.QueueInfo{exe("standard"), exe("frontier")})
	if err == nil || !strings.Contains(err.Error(), "standard, frontier") {
		t.Errorf("no match must list the real queues, got %v", err)
	}
	// Several resemble it → refuse rather than guess.
	_, err = resolveLiteralQueue("hpc1", "debug", "debug", []queue.QueueInfo{exe("cpu_debug"), exe("gpu_debug")})
	if err == nil || !strings.Contains(err.Error(), "cpu_debug, gpu_debug") {
		t.Errorf("ambiguity must list the candidates, got %v", err)
	}
	// No listing cached → nothing to check against; send the literal, as before.
	got, err = resolveLiteralQueue("hpc1", "debug", "debug", nil)
	if err != nil || got != "debug" {
		t.Errorf("no cache: got %q, %v", got, err)
	}
}
