package shellinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
)

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
hpc_user = "alice"
[[cluster]]
name = "alpha"
domain = "alpha.example.mil"
nodes = ["hpc2", "hpc1"]
[[cluster]]
name = "beta"
domain = "beta.example.mil"
nodes = ["node2"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	t.Setenv("MU_NODE", "hpc1") // this shell is "on" hpc1 → its dispatcher is skipped
	config.ResetForTest()       // config memoizes per-process; reload from this file

	out := Generate()

	// Config exports (the bridge that lets config.env be retired).
	for _, want := range []string{
		`export MU_HPC_UNAME="alice"`,
		`export MU_CLUSTERS="alpha beta"`,
		`export MU_CLUSTER_ALPHA_DOMAIN="alpha.example.mil"`,
		`export MU_CLUSTER_ALPHA_NODES="hpc1 hpc2"`, // nodes sorted
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing export %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "_mu_node() {") {
		t.Error("missing shared dispatcher helper")
	}
	// The dispatcher grammar (help arm, numbered-node selector) is verified
	// behaviorally by TestDispatchExec, which runs the generated code — a text
	// match here would just duplicate that, more brittly.
	if !strings.Contains(out, `hpc2() { _mu_node hpc2 "alice@hpc2.alpha.example.mil" "$@"; }`) {
		t.Errorf("missing/wrong hpc2 wrapper:\n%s", out)
	}
	if !strings.Contains(out, `node2() { _mu_node node2 "alice@node2.beta.example.mil" "$@"; }`) {
		t.Errorf("missing node2 wrapper:\n%s", out)
	}
	if strings.Contains(out, "hpc1()") {
		t.Error("self node (hpc1) should be skipped")
	}
}
