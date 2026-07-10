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
