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
nodes = ["mike", "login-c"]
[[cluster]]
name = "beta"
domain = "beta.example.mil"
nodes = ["node2"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	t.Setenv("MU_NODE", "login-c") // this shell is "on" login-c → its dispatcher is skipped
	config.ResetForTest()       // config memoizes per-process; reload from this file

	out := Generate()

	// Config exports (the bridge that lets config.env be retired).
	for _, want := range []string{
		`export MU_HPC_UNAME="alice"`,
		`export MU_CLUSTERS="alpha beta"`,
		`export MU_CLUSTER_ALPHA_DOMAIN="alpha.example.mil"`,
		`export MU_CLUSTER_ALPHA_NODES="login-c mike"`, // nodes sorted
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing export %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "_mu_node() {") {
		t.Error("missing shared dispatcher helper")
	}
	// Dispatcher grammar: help arm and the numbered-login-node selector.
	for _, want := range []string{
		"_mu_node_help() {",
		"-h|--help) _mu_node_help",
		`*) target="${target%%.*}$(printf '%02d' "$1").${target#*.}"; shift ;;`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing dispatcher grammar %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, `mike() { _mu_node mike "alice@mike.alpha.example.mil" "$@"; }`) {
		t.Errorf("missing/wrong mike wrapper:\n%s", out)
	}
	if !strings.Contains(out, `node2() { _mu_node node2 "alice@node2.beta.example.mil" "$@"; }`) {
		t.Errorf("missing node2 wrapper:\n%s", out)
	}
	if strings.Contains(out, "login-c()") {
		t.Error("self node (login-c) should be skipped")
	}
}
