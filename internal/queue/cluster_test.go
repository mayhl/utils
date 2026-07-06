package queue

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestJobClusterJSON: the Cluster field is omitted from JSON for single-cluster
// fetches (empty) and present only under cross-cluster collate — so the --json data
// contract stays clean for the common case.
func TestJobClusterJSON(t *testing.T) {
	b, err := json.Marshal(Job{ShortID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "cluster") {
		t.Errorf("empty cluster should be omitted: %s", b)
	}
	b, err = json.Marshal(Job{ShortID: "1", Cluster: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"cluster":"alpha"`) {
		t.Errorf("set cluster should marshal: %s", b)
	}
}
