package cli

import (
	"errors"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// TestMergeResults checks the fan-out's pure core: successful clusters' jobs
// flatten in order, hook progress merges under label-qualified keys (short ids
// collide across systems), and a failed cluster becomes one "cluster: reason"
// note rather than dropping the whole collate.
func TestMergeResults(t *testing.T) {
	results := []clusterResult{
		{cluster: "alpha", jobs: []queue.Job{{ShortID: "1"}, {ShortID: "2"}}, prog: map[string]string{"1": "38%"}},
		{cluster: "beta", err: errors.New("timeout after 20s")},
		{cluster: "gamma", jobs: []queue.Job{{ShortID: "1"}}, prog: map[string]string{"1": "90%"}},
	}
	jobs, prog, down := mergeResults(results)
	if len(jobs) != 3 {
		t.Errorf("merged jobs = %d, want 3", len(jobs))
	}
	if len(down) != 1 || down[0] != "beta: timeout after 20s" {
		t.Errorf("down = %v, want [beta: timeout after 20s]", down)
	}
	if prog["alpha/1"] != "38%" || prog["gamma/1"] != "90%" || len(prog) != 2 {
		t.Errorf("prog = %v, want colliding id 1 kept apart by label", prog)
	}
}

// TestApplyFleetHookProgress: merged rows pick up progress only through their own
// cluster's key — the same short id on another system must not bleed over.
func TestApplyFleetHookProgress(t *testing.T) {
	rows := []render.JobRow{
		{ID: "1", Cluster: "alpha"},
		{ID: "1", Cluster: "gamma"},
		{ID: "2", Cluster: "alpha"},
	}
	applyFleetHookProgress(rows, map[string]string{"alpha/1": "38%", "gamma/1": "90%"})
	if rows[0].Prog != "38%" || rows[1].Prog != "90%" || rows[2].Prog != "" {
		t.Errorf("rows = %+v, want 38%% / 90%% / empty", rows)
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
