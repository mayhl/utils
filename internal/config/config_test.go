package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// reset clears the once-cached config so a test can point at a fresh file/env.
func reset() {
	loadOnce = sync.Once{}
	loaded = nil
}

func TestTOMLReading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
hpc_user = "alice"

[transfer]
rsync_opts        = "-avz"
ssh_transfer_opts = "-qq"

[sshfs]
root = "/mnt/hpc"

[[cluster]]
name   = "alpha"
domain = "alpha.example.mil"
nodes  = ["hpc2", "hpc3"]

[[cluster]]
name   = "beta"
domain = "beta.example.mil"
nodes  = ["node2"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	reset()

	if User() != "alice" {
		t.Errorf("User = %q", User())
	}
	if RsyncOpts() != "-avz" {
		t.Errorf("RsyncOpts = %q", RsyncOpts())
	}
	if SSHTransferOpts() != "-qq" {
		t.Errorf("SSHTransferOpts = %q", SSHTransferOpts())
	}
	if SSHFSRoot() != "/mnt/hpc" {
		t.Errorf("SSHFSRoot = %q", SSHFSRoot())
	}

	defs := ClusterDefs()
	if len(defs) != 2 || defs[0].Name != "alpha" || defs[1].Name != "beta" {
		t.Fatalf("clusters (order should be preserved) = %+v", defs)
	}
	if tg := NodeTargets(); tg["hpc2"] != "alice@hpc2.alpha.example.mil" {
		t.Errorf("NodeTargets[hpc2] = %q", tg["hpc2"])
	}
}

func TestNoConfigUsesDefaults(t *testing.T) {
	// No config.toml (env encoding is retired) → empty clusters + built-in
	// scalar defaults. MU_ROOT is cleared so the dev shell's real config.toml
	// isn't picked up.
	t.Setenv("MU_CONFIG_FILE", "")
	t.Setenv("MU_ROOT", "")
	reset()

	if User() != "?" {
		t.Errorf("User = %q, want ?", User())
	}
	if defs := ClusterDefs(); len(defs) != 0 {
		t.Errorf("ClusterDefs = %+v, want empty", defs)
	}
	if RsyncOpts() != "-au --partial" {
		t.Errorf("RsyncOpts = %q", RsyncOpts())
	}
	if SSHTransferOpts() != "-q" {
		t.Errorf("SSHTransferOpts = %q", SSHTransferOpts())
	}
	if SSHFSRoot() != "~/hpc_sshfs" {
		t.Errorf("SSHFSRoot = %q", SSHFSRoot())
	}
}

func TestClusterSchedulerActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[[cluster]]
name = "slurmy"
domain = "s.example.mil"
nodes = ["hpc1"]
scheduler = "SLURM"

[[cluster]]
name = "pbsy"
domain = "p.example.mil"
nodes = ["hpc2"]
scheduler = "pbs"
active = false
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	reset()

	if got := SchedulerFor("hpc1"); got != "slurm" { // lower-cased
		t.Errorf("SchedulerFor(hpc1) = %q", got)
	}
	if got := SchedulerFor("hpc2"); got != "pbs" {
		t.Errorf("SchedulerFor(hpc2) = %q", got)
	}
	if got := SchedulerFor("ghost"); got != "" {
		t.Errorf("SchedulerFor(unknown) = %q", got)
	}
	if act := ActiveClusters(); len(act) != 1 || act[0].Name != "slurmy" {
		t.Errorf("ActiveClusters (pbsy is active=false) = %+v", act)
	}
}

func TestSSHCommandIsEnvSeam(t *testing.T) {
	t.Setenv("MU_SSH", "ossh")
	if SSHCommand() != "ossh" {
		t.Errorf("SSHCommand = %q", SSHCommand())
	}
	t.Setenv("MU_SSH", "")
	if SSHCommand() != "ssh" {
		t.Errorf("SSHCommand default = %q", SSHCommand())
	}
}
