package shellinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// TestDispatchExec sources the generated dispatcher in a real shell and exercises
// the grammar end-to-end, so a parse/dispatch regression is caught (the string
// assertions in TestGenerate only pin the codegen text). It runs under both bash
// and zsh — the two shells the dispatcher must stay portable across — skipping a
// shell that isn't installed. The seam helpers are stubbed to echo what they would
// do (mu_auth ok, mu_ssh_login/mu/$MU_SSH print their target/args).
func TestDispatchExec(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	body := `
hpc_user = "alice"
[[cluster]]
name = "alpha"
domain = "alpha.example.mil"
nodes = ["login-a"]
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", cfg)
	t.Setenv("MU_NODE", "none") // this shell isn't login-a → its dispatcher is emitted
	config.ResetForTest()       // config memoizes per-process; reload from this file

	// Stub the framework seams, source the generated dispatchers, then run every
	// grammar arm. mu_ssh_login prints CONNECT <target>; $MU_SSH (fakessh) prints
	// SSH <target> :: <remote-cmd> so numbered remote-exec is observable too.
	driver := `
mu_auth() { return 0; }
mu_ssh_login() { echo "CONNECT $1"; }
mu() { echo "MU $*"; }
fakessh() { echo "SSH $2 :: $3"; }
export MU_SSH=fakessh
` + Generate() + `
login-a
login-a 3
login-a 12
login-a uptime
login-a 3 uptime
login-a push a b
login-a -h
`
	script := filepath.Join(dir, "driver.sh")
	if err := os.WriteFile(script, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}

	wants := []string{
		"CONNECT alice@login-a.alpha.example.mil",   // bare connect
		"CONNECT alice@login-a03.alpha.example.mil", // numbered connect (N=3, zero-padded)
		"CONNECT alice@login-a12.alpha.example.mil", // two-digit number passes through
		"SSH alice@login-a.alpha.example.mil :: ",   // remote-exec, default login
		"SSH alice@login-a03.alpha.example.mil :: ", // remote-exec on numbered login node
		"MU cp push login-a a b",                    // push stays node-level
		"connect to login node N",                   // -h prints the grammar
	}

	for _, sh := range []string{"bash", "zsh"} {
		t.Run(sh, func(t *testing.T) {
			bin, err := exec.LookPath(sh)
			if err != nil {
				t.Skipf("%s not on PATH", sh)
			}
			out, err := exec.Command(bin, script).CombinedOutput()
			if err != nil {
				t.Fatalf("%s exited with error: %v\n%s", sh, err, out)
			}
			got := string(out)
			for _, w := range wants {
				if !strings.Contains(got, w) {
					t.Errorf("%s: missing %q in output:\n%s", sh, w, got)
				}
			}
		})
	}
}
