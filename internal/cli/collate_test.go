package cli

import (
	"errors"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
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

// TestClusterTargets: one target per cluster, labeled by cluster name, fetching its
// representative (first) node; a cluster with no nodes yields an empty node so
// fetchTarget reports it rather than dropping it silently.
func TestClusterTargets(t *testing.T) {
	got := clusterTargets([]config.Cluster{
		{Name: "dsrc1", Scheduler: "pbs", Nodes: []string{"node-a", "node-b"}},
		{Name: "dsrc2", Scheduler: "", Nodes: nil},
	})
	if len(got) != 2 {
		t.Fatalf("targets = %d, want 2: %+v", len(got), got)
	}
	if got[0] != (queueTarget{label: "dsrc1", scheduler: "pbs", node: "node-a"}) {
		t.Errorf("dsrc1 target = %+v", got[0])
	}
	if got[1] != (queueTarget{label: "dsrc2", scheduler: "", node: ""}) {
		t.Errorf("dsrc2 (no nodes) target = %+v", got[1])
	}
}
