package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
)

// Account cache — the CONFIG panel only.
//
// The set of allocations you can charge a job to is exactly the set `show_usage` lists, so
// `mu config -i` offers those subprojects as a picker for `account` rather than asking you
// to retype a code. But a config edit must work on a laptop with no ticket: opening an
// editor is no reason to fire pkinit at the realm. So the panel never fetches — it reads
// this cache, which `mu hpc usage` writes on every live fetch. No cache, no picker: the
// field stays free text.
//
// Only the NAMES are cached. Hours and percent-remaining are stale the moment they're
// written, and `mu hpc usage` exists to show those — nothing here may reach a table.
//
// A month, because an allocation is annual: a list a few weeks old still names your
// subprojects, where a queue inventory turns over with the machine's maintenance.
const acctCacheTTL = 30 * 24 * time.Hour

// acctCacheEntry is one machine's cached subprojects, stamped so it can go stale.
type acctCacheEntry struct {
	Fetched  time.Time `json:"fetched"`
	Accounts []string  `json:"accounts"`
}

// acctCachePath is where a machine's subprojects live. Disposable (a lost file just costs
// the picker until the next `mu hpc usage`), so ~/.cache, beside the queue listings.
func acctCachePath(label string) string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "mayhl_utils", "accounts", label+".json")
}

// writeAcctCache stores the subprojects a live `show_usage` listed for label. A fetch that
// parsed nothing is not cached — the next run should retry, not inherit an empty picker for
// a month. Best-effort: a cache mu couldn't write is not worth failing a usage report over.
func writeAcctCache(label string, infos []queue.UsageInfo) {
	if label == "" {
		return
	}
	var accts []string
	for _, in := range infos {
		if s := strings.TrimSpace(in.Subproject); s != "" && !slices.Contains(accts, s) {
			accts = append(accts, s)
		}
	}
	if len(accts) == 0 {
		return
	}
	slices.Sort(accts)
	b, err := json.Marshal(acctCacheEntry{Fetched: time.Now(), Accounts: accts})
	if err != nil {
		return
	}
	p := acctCachePath(label)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o644)
}

// readAcctCache returns label's cached subprojects, or nil if there is no entry, it is
// older than acctCacheTTL, or it is unreadable — every miss just drops back to free text.
func readAcctCache(label string, now time.Time) []string {
	b, err := os.ReadFile(acctCachePath(label))
	if err != nil {
		return nil
	}
	var e acctCacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil
	}
	if now.Sub(e.Fetched) > acctCacheTTL || len(e.Accounts) == 0 {
		return nil
	}
	return e.Accounts
}

// clusterAccounts is the picker's source for a cluster: the union of what its machines have
// cached. The cache is keyed by the machine mu actually ran `show_usage` on, but an
// allocation is a DSRC-wide fact, so any one of a cluster's machines can answer for it —
// and running `mu hpc usage` on one machine shouldn't leave its siblings without a picker.
func clusterAccounts(cluster string, now time.Time) []string {
	var out []string
	for _, c := range config.ClusterDefs() {
		if c.Name != cluster {
			continue
		}
		for _, n := range c.Nodes {
			for _, a := range readAcctCache(n, now) {
				if !slices.Contains(out, a) {
					out = append(out, a)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}
