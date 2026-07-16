package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mayhl/mayhl_utils/internal/queue"
)

// Hours cache — the submit PRE-FLIGHT only.
//
// The account cache (acctcache.go) deliberately keeps only subproject NAMES, because its
// consumer — the offline config picker — must never imply live hours. The pre-flight is a
// different consumer: an advisory guardrail that ALWAYS stamps how old its number is, so it
// may cache the hours themselves. Short-lived (an hour), and the caller force-refreshes at
// the edge of an allocation, where a stale number is exactly what misleads — see hoursPreflight.
const usageCacheTTL = time.Hour

// usageCacheEntry is one machine's last show_usage rows, hours and all, stamped so the
// pre-flight can age it and decide whether to re-fetch.
type usageCacheEntry struct {
	Fetched time.Time         `json:"fetched"`
	Rows    []queue.UsageInfo `json:"rows"`
}

// usageCachePath sits beside the account cache under ~/.cache — disposable, a miss just
// means the pre-flight fetches live (or, off-ticket, degrades to no percentages).
func usageCachePath(label string) string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "mayhl_utils", "usage", label+".json")
}

// writeUsageCache stores the rows a live show_usage returned for label. Best-effort — a
// cache mu couldn't write is no reason to fail a submit; the next pre-flight just fetches.
func writeUsageCache(label string, rows []queue.UsageInfo) {
	if label == "" || len(rows) == 0 {
		return
	}
	b, err := json.Marshal(usageCacheEntry{Fetched: time.Now(), Rows: rows})
	if err != nil {
		return
	}
	p := usageCachePath(label)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o644)
}

// readUsageCache returns label's cached rows and their age; ok=false when there is no
// entry or it is unreadable. Staleness is the CALLER's call (fresh under the TTL, or forced
// live at an allocation's edge), so this reports the age rather than dropping stale to nil —
// unlike the account cache, whose every miss is equal.
func readUsageCache(label string, now time.Time) (rows []queue.UsageInfo, age time.Duration, ok bool) {
	b, err := os.ReadFile(usageCachePath(label))
	if err != nil {
		return nil, 0, false
	}
	var e usageCacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, 0, false
	}
	if len(e.Rows) == 0 {
		return nil, 0, false
	}
	return e.Rows, now.Sub(e.Fetched), true
}
