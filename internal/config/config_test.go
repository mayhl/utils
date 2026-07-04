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
nodes  = ["mike", "login-c"]

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
	if tg := NodeTargets(); tg["mike"] != "alice@mike.alpha.example.mil" {
		t.Errorf("NodeTargets[mike] = %q", tg["mike"])
	}
}

func TestEnvFallback(t *testing.T) {
	t.Setenv("MU_CONFIG_FILE", "") // no file → env
	t.Setenv("MU_ROOT", "")
	t.Setenv("MU_HPC_UNAME", "bob")
	t.Setenv("MU_CLUSTERS", "alpha")
	t.Setenv("MU_CLUSTER_ALPHA_DOMAIN", "alpha.example.mil")
	t.Setenv("MU_CLUSTER_ALPHA_NODES", "mike login-c")
	// Clear scalars the dev shell may have exported, so the default-fallback
	// assertions below test the true defaults, not inherited values.
	t.Setenv("MU_HPC_RSYNC_OPTS", "")
	t.Setenv("MU_SSHFS_ROOT", "")
	reset()

	if User() != "bob" {
		t.Errorf("User = %q", User())
	}
	defs := ClusterDefs()
	if len(defs) != 1 || len(defs[0].Nodes) != 2 {
		t.Fatalf("env clusters = %+v", defs)
	}
	// Unset scalars fall back to defaults.
	if RsyncOpts() != "-au --partial" || SSHFSRoot() != "~/hpc_sshfs" {
		t.Errorf("defaults wrong: %q / %q", RsyncOpts(), SSHFSRoot())
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
