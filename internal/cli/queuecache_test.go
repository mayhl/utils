package cli

import (
	"testing"
	"time"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// TestQueueCache pins what the cache is FOR: the inventory survives a round trip, the live
// counts don't, and a stale entry is a miss rather than a lie.
func TestQueueCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	now := time.Now()
	qs := []queue.QueueInfo{{
		Name: "standard", MaxWalltime: "168:00:00", MaxCores: "8192", Type: "Exe",
		Enabled: "E", Running: "R", JobsRun: "12", JobsPend: "3", CoresRun: "500", CoresPend: "64",
	}}

	writeQueueCache("hpc1", qs)
	got := readQueueCache("hpc1", now)
	if len(got) != 1 {
		t.Fatalf("read back %d queues, want 1", len(got))
	}
	if got[0].Name != "standard" || got[0].MaxWalltime != "168:00:00" || got[0].Type != "Exe" {
		t.Errorf("inventory lost in the round trip: %+v", got[0])
	}
	// The counts are live state; a cached one would be a stale number rendered as fact.
	if got[0].JobsRun != "" || got[0].JobsPend != "" || got[0].CoresRun != "" || got[0].CoresPend != "" {
		t.Errorf("cached the live counts: %+v", got[0])
	}
	// Past the TTL it's a miss, so the next read fetches.
	if readQueueCache("hpc1", now.Add(queueCacheTTL+time.Minute)) != nil {
		t.Error("a stale entry was served")
	}
	if readQueueCache("hpc2", now) != nil {
		t.Error("served an entry for a cluster never cached")
	}
	// A broken fetch parses to nothing — don't cache that, or it sticks for a day.
	writeQueueCache("hpc2", nil)
	if readQueueCache("hpc2", now) != nil {
		t.Error("cached an empty listing")
	}
}
