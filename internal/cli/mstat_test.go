package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// TestCurrentCluster covers the on-cluster mstat resolution: $MU_NODE over $BC_HOST,
// the login-node digit-strip fallback (login-a01 → login-a), and the off-HPC and
// unconfigured-scheduler cases.
func TestCurrentCluster(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	body := `hpc_user = "me"
[[cluster]]
name = "alpha"
domain = "alpha.example"
scheduler = "slurm"
nodes = ["login-a"]
[[cluster]]
name = "beta"
domain = "beta.example"
nodes = ["login-b"]
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", cfg)
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	cases := []struct {
		name           string
		muNode, bcHost string
		wantName       string
		wantSched      string
	}{
		{"exact BC_HOST", "", "login-a", "login-a", "slurm"},
		{"login-node digit strip", "", "login-a01", "login-a", "slurm"},
		{"MU_NODE overrides BC_HOST", "login-a", "login-b", "login-a", "slurm"},
		{"off-HPC (nothing set)", "", "", "", ""},
		{"on-HPC but scheduler unconfigured", "", "login-b", "login-b", ""},
		{"on-HPC but node unknown", "", "mystery", "mystery", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MU_NODE", c.muNode)
			t.Setenv("BC_HOST", c.bcHost)
			name, sched := currentCluster()
			if name != c.wantName || sched != c.wantSched {
				t.Errorf("currentCluster() = (%q, %q), want (%q, %q)", name, sched, c.wantName, c.wantSched)
			}
		})
	}
}
