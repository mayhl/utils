package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// Queue-list cache — the SUBMIT path only.
//
// `mu job sub` wants the queue INVENTORY: names, limits, class, up/down. Those are facts
// about how the center configured the machine, and each read of them costs an ssh round
// trip in front of a user who is waiting on a form. So the submit path reads a cached
// listing and only fetches when it is missing or stale.
//
// The live counts (JobsRun/Pend, CoresRun/Pend) are NOT inventory — they are wrong the
// moment they are written — so they are dropped on the way in, and no cached count can
// reach a table. `mu hpc queues` exists to show those counts, so it never READS the cache;
// but every live fetch it makes writes one, which makes the view the manual refresh:
//
//	mu hpc queues -N hpc1
const queueCacheTTL = 24 * time.Hour

// queueCacheEntry is one cluster's cached listing, stamped so it can go stale.
type queueCacheEntry struct {
	Fetched time.Time         `json:"fetched"`
	Queues  []queue.QueueInfo `json:"queues"`
}

// queueCachePath is where a cluster's listing lives. Disposable (a lost file just costs
// one fetch), so ~/.cache, like doctor's checkup stamp.
func queueCachePath(label string) string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "mayhl_utils", "queues", label+".json")
}

// cachedQueues is the submit path's queue-list read: the cached listing if it is fresh,
// else a live fetch that refreshes it. QUIET — it renders nothing (it runs under the sub
// form's TUI), so the caller shapes any failure.
func cachedQueues(node string) (string, []queue.QueueInfo, error) {
	label, err := queueLabel(node)
	if err != nil {
		return "", nil, err
	}
	if qs := readQueueCache(label, time.Now()); qs != nil {
		return label, qs, nil
	}
	label, qs, err := showQueuesAt(node)
	if err != nil {
		return label, nil, err
	}
	writeQueueCache(label, qs)
	return label, qs, nil
}

// readQueueCache returns label's cached queues, or nil if there is no entry, it is older
// than queueCacheTTL, or it is unreadable — every miss is just a fetch.
func readQueueCache(label string, now time.Time) []queue.QueueInfo {
	b, err := os.ReadFile(queueCachePath(label))
	if err != nil {
		return nil
	}
	var e queueCacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil
	}
	if now.Sub(e.Fetched) > queueCacheTTL || len(e.Queues) == 0 {
		return nil
	}
	return e.Queues
}

// writeQueueCache stores a live listing as label's inventory, stripping the live counts
// so a stale one can never be rendered. A broken fetch (no queues) is not cached — the
// next call should retry, not inherit an empty list for a day. Best-effort: a cache the
// tool couldn't write is not worth failing a submit over.
func writeQueueCache(label string, qs []queue.QueueInfo) {
	if label == "" || len(qs) == 0 {
		return
	}
	inv := make([]queue.QueueInfo, len(qs))
	for i, q := range qs {
		q.JobsRun, q.JobsPend, q.CoresRun, q.CoresPend = "", "", "", ""
		inv[i] = q
	}
	b, err := json.Marshal(queueCacheEntry{Fetched: time.Now(), Queues: inv})
	if err != nil {
		return
	}
	p := queueCachePath(label)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o644)
}
