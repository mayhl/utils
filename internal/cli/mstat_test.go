package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// TestValidUserList: -u accepts a comma-separated username list; rejects empty and
// anything with shell-unsafe characters (it's interpolated into the fetch command).
func TestValidUserList(t *testing.T) {
	for _, s := range []string{"alice", "alice,bob", "a.b_c-d", "user01,user02"} {
		if !validUserList(s) {
			t.Errorf("validUserList(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "alice bob", "alice;rm", "a|b", "$(x)", "a,b c"} {
		if validUserList(s) {
			t.Errorf("validUserList(%q) = true, want false", s)
		}
	}
}

// TestFetchSpecWho: the WHO axis picks the right user filter per scheduler — a -u list,
// all users (-a), or just you (PBS names you explicitly from config; SLURM uses --me).
func TestFetchSpecWho(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("hpc_user = \"me\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", cfg)
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	cases := []struct {
		sched string
		who   userSel
		want  string
	}{
		{"slurm", userSel{}, `squeue -h --me -o "%i|%P|%j|%u|%t|%M|%l|%D|%R|%S"`},
		{"slurm", userSel{all: true}, `squeue -h -o "%i|%P|%j|%u|%t|%M|%l|%D|%R|%S"`},
		{"slurm", userSel{list: "alice,bob"}, `squeue -h -u alice,bob -o "%i|%P|%j|%u|%t|%M|%l|%D|%R|%S"`},
		{"pbs", userSel{}, "qstat -a -u me"},
		{"pbs", userSel{all: true}, "qstat -a"},
		{"pbs", userSel{list: "alice,bob"}, "qstat -a -u alice,bob"},
	}
	for _, c := range cases {
		if cmd, _ := fetchSpec(c.sched, c.who); cmd != c.want {
			t.Errorf("fetchSpec(%q, %+v) = %q, want %q", c.sched, c.who, cmd, c.want)
		}
	}
}

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
