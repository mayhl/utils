package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// TestAcctCache locks what the account picker rests on: names survive the round trip and
// the hour figures do not, a listing that parsed nothing is not cached, and a stale entry
// reads as a miss (the picker degrades to free text rather than offering last year's
// allocations).
func TestAcctCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	writeAcctCache("hpc1", []queue.UsageInfo{
		{Subproject: "PROJ2", Allocated: "500000", Remaining: "120000"},
		{Subproject: "PROJ1", Used: "80000"},
		{Subproject: "PROJ1"}, // a repeat is one account, not two
		{Subproject: " "},     // and a blank is none
	})

	got := readAcctCache("hpc1", time.Now())
	if strings.Join(got, ",") != "PROJ1,PROJ2" {
		t.Errorf("cached accounts = %v, want [PROJ1 PROJ2] sorted and deduped", got)
	}
	// Nothing but the names may land on disk — an hours figure is stale the moment it's
	// written, and a cached one could reach a table.
	raw, err := os.ReadFile(acctCachePath("hpc1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "500000") || strings.Contains(string(raw), "120000") {
		t.Errorf("hour figures reached the cache: %s", raw)
	}

	if readAcctCache("hpc1", time.Now().Add(acctCacheTTL+time.Hour)) != nil {
		t.Error("a stale entry must read as a miss")
	}
	writeAcctCache("hpc2", nil)
	if readAcctCache("hpc2", time.Now()) != nil {
		t.Error("a listing that parsed nothing must not be cached")
	}
}

// TestClusterAccounts covers the picker's cluster view: one machine's fetch answers for its
// siblings (an allocation is DSRC-wide), a machine outside the cluster doesn't, and a
// cluster nobody has fetched from has no picker at all.
func TestClusterAccounts(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[[cluster]]
name = "alpha"
domain = "a.example.mil"
nodes = ["hpc1", "hpc2"]

[[cluster]]
name = "beta"
domain = "b.example.mil"
nodes = ["hpc3"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	config.ResetForTest()
	defer config.ResetForTest()

	writeAcctCache("hpc2", []queue.UsageInfo{{Subproject: "PROJ2"}, {Subproject: "PROJ1"}})
	writeAcctCache("hpc3", []queue.UsageInfo{{Subproject: "OTHER"}})

	// hpc1 was never fetched from, but it's alpha's machine — its cluster's picker stands.
	if got := clusterAccounts("alpha", time.Now()); strings.Join(got, ",") != "PROJ1,PROJ2" {
		t.Errorf("alpha accounts = %v, want beta's excluded and hpc2's union'd in", got)
	}
	if got := clusterAccounts("gamma", time.Now()); got != nil {
		t.Errorf("an unknown cluster has no picker, got %v", got)
	}
}

// TestAcctKey locks when the account field turns into a picker: only with a cache, only for
// `account`, and a node's picker leads with the blank that clears it back to the cluster's.
func TestAcctKey(t *testing.T) {
	accts := []string{"PROJ1", "PROJ2"}

	k := acctKey(strKey("account", "default allocation"), accts, false)
	if k.kind != render.FieldEnum || strings.Join(k.options, ",") != "PROJ1,PROJ2" {
		t.Errorf("cluster account = kind %v options %v", k.kind, k.options)
	}
	if n := acctKey(strKey("account", ""), accts, true); n.options[0] != "" {
		t.Errorf("node account options = %v, want a leading blank (inherit)", n.options)
	}
	if bare := acctKey(strKey("account", "h"), nil, false); bare.kind != render.FieldText || bare.hint != "h" {
		t.Error("no cache → the field stays free text, hint untouched")
	}
	if other := acctKey(strKey("domain", ""), accts, false); other.kind != render.FieldText {
		t.Error("only `account` becomes a picker")
	}
}
