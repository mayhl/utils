package config

import (
	"os"
	"path/filepath"
	"slices"
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
fleet = ["hpc2", "node2"]

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
	if fl := Fleet(); len(fl) != 2 || fl[0] != "hpc2" || fl[1] != "node2" {
		t.Errorf("Fleet = %v, want [hpc2 node2]", fl)
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

func TestSubmitQueueFor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[[cluster]]
name = "alpha"
domain = "a.example.mil"
nodes = ["hpc1"]
submit_queue = { default = "standard", GPU = "gpu_short" }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	reset()

	if got := SubmitQueueFor("hpc1", "default"); got != "standard" {
		t.Errorf("SubmitQueueFor(default) = %q", got)
	}
	if got := SubmitQueueFor("alpha", "gpu"); got != "gpu_short" { // by cluster name; key case-normalized both sides
		t.Errorf("SubmitQueueFor(gpu) = %q", got)
	}
	if got := SubmitQueueFor("hpc1", "vis"); got != "" {
		t.Errorf("SubmitQueueFor(unset key) = %q", got)
	}
	if got := SubmitQueueFor("ghost", "default"); got != "" {
		t.Errorf("SubmitQueueFor(unknown node) = %q", got)
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

func TestParseSize(t *testing.T) {
	good := map[string]int64{
		"1B":     1,
		"500KB":  500 << 10,
		"1.5MB":  3 << 19, // 1.5 * 1024^2
		"2GB":    2 << 30,
		"1TB":    1 << 40,
		" 1 GB ": 1 << 30,
		"2gb":    2 << 30,
		"1024":   1024, // bare bytes
	}
	for in, want := range good {
		if got, ok := parseSize(in); !ok || got != want {
			t.Errorf("parseSize(%q) = %d,%v want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "GB", "-1GB", "0", "1XB", "fast"} {
		if _, ok := parseSize(in); ok {
			t.Errorf("parseSize(%q) ok, want reject", in)
		}
	}
}

// TestNodeOverrides covers the per-machine scope: a DSRC's machines share a domain but
// not a queue plane, so every submit field falls back node → cluster, the maps merging
// per KEY (hpc2 overrides only `debug` and still inherits the cluster's `default`). A
// node block also declares membership, so an override-only machine needn't be listed twice.
func TestNodeOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[[cluster]]
name = "dsrc"
domain = "d.example.mil"
nodes = ["hpc1", "hpc2"]
scheduler = "pbs"
account = "CLUSTER-ALLOC"
cores_per_node = 128
queue_class = { odd = "GPU" }
queue_cores = { odd = 64 }
submit_queue = { default = "standard", debug = "cluster-debug" }

  [[cluster.node]]
  name = "hpc2"
  scheduler = "slurm"
  account = "NODE-ALLOC"
  cores_per_node = 192
  queue_class = { odd = "BigMem" }
  queue_cores = { odd = 96 }
  submit_queue = { DEBUG = "node-debug" }

  [[cluster.node]]
  name = "hpc3"
  cores_per_node = 48
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	reset()

	// hpc1 has no block → the cluster's values stand.
	for _, c := range []struct{ got, want string }{
		{SchedulerFor("hpc1"), "pbs"},
		{AccountFor("hpc1"), "CLUSTER-ALLOC"},
		{QueueClassOverride("hpc1", "odd"), "GPU"},
		{SubmitQueueFor("hpc1", "debug"), "cluster-debug"},
	} {
		if c.got != c.want {
			t.Errorf("hpc1: got %q, want %q", c.got, c.want)
		}
	}
	if got := CoresPerNodeFor("hpc1"); got != 128 {
		t.Errorf("CoresPerNodeFor(hpc1) = %d, want 128", got)
	}

	// hpc2's block wins field by field — including the scheduler.
	for _, c := range []struct{ got, want string }{
		{SchedulerFor("hpc2"), "slurm"},
		{AccountFor("hpc2"), "NODE-ALLOC"},
		{QueueClassOverride("hpc2", "odd"), "BigMem"},
		{SubmitQueueFor("hpc2", "debug"), "node-debug"}, // key lower-cased, as at cluster level
		{SubmitQueueFor("hpc2", "default"), "standard"}, // NOT overridden → inherited per key
	} {
		if c.got != c.want {
			t.Errorf("hpc2: got %q, want %q", c.got, c.want)
		}
	}
	if got := CoresPerNodeFor("hpc2"); got != 192 {
		t.Errorf("CoresPerNodeFor(hpc2) = %d, want 192", got)
	}
	if got := QueueCoresOverride("hpc2", "odd"); got != 96 {
		t.Errorf("QueueCoresOverride(hpc2) = %d, want 96", got)
	}

	// hpc3 exists ONLY as a node block — it still joins the cluster.
	defs := ClusterDefs()
	if len(defs) != 1 || !slices.Equal(defs[0].Nodes, []string{"hpc1", "hpc2", "hpc3"}) {
		t.Errorf("Nodes = %v, want the node blocks folded in and sorted", defs[0].Nodes)
	}
	if got := SchedulerFor("hpc3"); got != "pbs" { // no override → the cluster's
		t.Errorf("SchedulerFor(hpc3) = %q", got)
	}
	if got := CoresPerNodeFor("hpc3"); got != 48 {
		t.Errorf("CoresPerNodeFor(hpc3) = %d, want 48", got)
	}
}
