package cli

import (
	"errors"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// TestMergeResults checks the fan-out's pure core: successful clusters' jobs
// flatten in order, and a failed cluster becomes one "cluster: reason" note rather
// than dropping the whole collate.
func TestMergeResults(t *testing.T) {
	results := []clusterResult{
		{cluster: "alpha", jobs: []queue.Job{{ShortID: "1"}, {ShortID: "2"}}},
		{cluster: "beta", err: errors.New("timeout after 20s")},
		{cluster: "gamma", jobs: []queue.Job{{ShortID: "3"}}},
	}
	jobs, down := mergeResults(results)
	if len(jobs) != 3 {
		t.Errorf("merged jobs = %d, want 3", len(jobs))
	}
	if len(down) != 1 || down[0] != "beta: timeout after 20s" {
		t.Errorf("down = %v, want [beta: timeout after 20s]", down)
	}
}
