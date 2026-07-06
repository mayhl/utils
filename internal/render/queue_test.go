package render

import "testing"

// TestAnyCluster: the Cluster column shows only when a row carries a cluster tag
// (a cross-cluster collate), and stays hidden for single-cluster views.
func TestAnyCluster(t *testing.T) {
	if anyCluster([]JobRow{{ID: "1"}, {ID: "2"}}) {
		t.Error("no cluster tags → want false")
	}
	if !anyCluster([]JobRow{{ID: "1"}, {ID: "2", Cluster: "alpha"}}) {
		t.Error("a cluster tag present → want true")
	}
}
