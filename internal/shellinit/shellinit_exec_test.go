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
nodes = ["hpc1"]
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", cfg)
	t.Setenv("MU_NODE", "none") // this shell isn't hpc1 → its dispatcher is emitted
	config.ResetForTest()       // config memoizes per-process; reload from this file

	// Stub the framework seams, source the generated dispatchers, then run every
	// grammar arm. mu_ssh_login prints CONNECT <target>; $MU_SSH (fakessh) prints
	// SSH <target> :: <remote-cmd> so numbered remote-exec is observable too, and
	// writes two stderr lines — a benign dbus line (must be dropped by the
	// dispatcher's stderr filter) and a real error (must survive it).
	driver := `
mu_auth() { return 0; }
mu_ssh_login() { echo "CONNECT $1"; }
mu() { echo "MU $*"; }
fakessh() {
  echo "SSH $2 :: $3"
  echo "dbus-update-activation-environment: noise" >&2
  echo "real-error-boom" >&2
}
export MU_SSH=fakessh
` + Generate() + `
hpc1
hpc1 3
hpc1 12
hpc1 uptime
hpc1 3 uptime
hpc1 push a b
hpc1 -h
`
	script := filepath.Join(dir, "driver.sh")
	if err := os.WriteFile(script, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}

	wants := []string{
		"CONNECT alice@hpc1.alpha.example.mil",   // bare connect
		"CONNECT alice@hpc103.alpha.example.mil", // numbered connect (N=3, zero-padded)
		"CONNECT alice@hpc112.alpha.example.mil", // two-digit number passes through
		"SSH alice@hpc1.alpha.example.mil :: ",   // remote-exec, default login
		"SSH alice@hpc103.alpha.example.mil :: ", // remote-exec on numbered login node
		"MU cp push hpc1 a b",                    // push stays node-level
		"connect to login node N",                // -h prints the grammar
		"real-error-boom",                        // a real stderr line survives the filter
	}
	// The benign dbus login-profile noise must be dropped by the stderr filter.
	notWants := []string{"dbus-update-activation-environment"}

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
			for _, nw := range notWants {
				if strings.Contains(got, nw) {
					t.Errorf("%s: %q should have been filtered from stderr but appeared:\n%s", sh, nw, got)
				}
			}
		})
	}
}
