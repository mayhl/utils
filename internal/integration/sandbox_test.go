//go:build sandbox

package integration

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// muBin is the mu binary built once for the suite; skipReason is set when the sandbox
// prerequisites aren't met, in which case every test t.Skips.
var (
	muBin      string
	skipReason string
)

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		skipReason = err.Error()
	}
	code := m.Run()
	if muBin != "" {
		_ = os.RemoveAll(filepath.Dir(muBin))
	}
	os.Exit(code)
}

// setup checks the sandbox is usable and builds a native mu (it runs here on the mac and
// sshes to the box). Any failure becomes a skip, not a hard error.
func setup() error {
	if os.Getenv("MU_SANDBOX_CONFIG") == "" {
		// Default to the in-repo rig config (this test's CWD is the package dir), so
		// `go test -tags sandbox ./internal/integration/` works with no env var. Absolute,
		// so the mu subprocess resolves it regardless of its own CWD.
		abs, err := filepath.Abs("sandbox/test-config.toml")
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return errors.New("no sandbox/test-config.toml (and MU_SANDBOX_CONFIG unset)")
		}
		if err := os.Setenv("MU_SANDBOX_CONFIG", abs); err != nil {
			return err
		}
	}
	if err := exec.Command("ssh", "sandbox", "true").Run(); err != nil {
		return errors.New("box unreachable — cd ~/.config/sandbox && docker compose up -d")
	}
	dir, err := os.MkdirTemp("", "mu-sandbox")
	if err != nil {
		return err
	}
	bin := filepath.Join(dir, "mu")
	b := exec.Command("go", "build", "-o", bin, "./cmd/mu")
	b.Dir = "../.." // package dir → repo root
	if out, err := b.CombinedOutput(); err != nil {
		return fmt.Errorf("build mu: %w\n%s", err, out)
	}
	muBin = bin
	return nil
}

func requireSandbox(t *testing.T) {
	t.Helper()
	if skipReason != "" {
		t.Skip("sandbox: " + skipReason)
	}
}

// muEnv builds the environment mu runs under against the sandbox. Critically it sets
// MU_SYSTEM=hpc so EnsureTicket short-circuits (the box uses ssh key auth — mu must
// NEVER pkinit against the real Kerberos realm), and prepends the sandbox bin/ (a no-op
// pkinit stub) as a second guard. Inherited MU_* / PATH that would interfere are dropped.
func muEnv() []string {
	sandboxBin := filepath.Join(filepath.Dir(os.Getenv("MU_SANDBOX_CONFIG")), "bin")
	drop := map[string]bool{
		"PATH": true, "MU_CONFIG_FILE": true, "MU_RENDER": true,
		"MU_SYSTEM": true, "MU_HPC_UNAME": true, "MU_SSH": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && drop[kv[:i]] {
			continue
		}
		env = append(env, kv)
	}
	return append(
		env,
		"MU_CONFIG_FILE="+os.Getenv("MU_SANDBOX_CONFIG"),
		"MU_RENDER=plain", // borderless, parseable
		"MU_SYSTEM=hpc",   // key-auth box → EnsureTicket no-ops; no real pkinit
		"PATH="+sandboxBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
}

// mu runs the built binary against the sandbox config, and fails the test on a non-zero
// exit (folding stdout+stderr into the message).
func mu(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(muBin, args...)
	cmd.Env = muEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mu %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// muCfg is mu() but pointed at a specific config.toml (overriding MU_CONFIG_FILE) — used by
// the pull test so it writes a temp copy instead of the shared fixture.
func muCfg(t *testing.T, cfg string, args ...string) string {
	t.Helper()
	env := muEnv()
	for i, kv := range env {
		if strings.HasPrefix(kv, "MU_CONFIG_FILE=") {
			env[i] = "MU_CONFIG_FILE=" + cfg
		}
	}
	cmd := exec.Command(muBin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mu %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in output:\n%s", w, out)
		}
	}
}

// gitOut runs a git command in dir and returns its output, failing the test on error.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestQueuePBS drives `mu hpc queue --node sandbox` — PBS idiom (qstat -a) — and checks
// the fake queue's jobs render.
func TestQueuePBS(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "--node", "sandbox")
	mustContain(t, out, "1284570", "run_wave", "post_proc", "nest_grid")
}

// TestQueueSLURM drives `mu hpc queue --node sandslurm` — SLURM idiom (squeue -o pipe).
func TestQueueSLURM(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "--node", "sandslurm")
	mustContain(t, out, "8359638", "run_wave", "mesh_gen", "nest_grid")
}

// TestQueuesPBS drives `mu hpc queues --node sandbox` through the box's show_queues stub.
// Default view: up Exe queues survive (standard, with the box-distinct run count 17 and
// MaxNodes 32 from sbpbs cores_per_node=128), routing/down queues are filtered with the
// not-up warning; -a brings them back.
func TestQueuesPBS(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queues", "--node", "sandbox")
	mustContain(t, out, "standard", "168:00:00", "17", "32", "not up")
	if strings.Contains(out, "route") || strings.Contains(out, "frozen") {
		t.Errorf("routing/down queue leaked into the default view:\n%s", out)
	}
	all := mu(t, "hpc", "queues", "--node", "sandbox", "-a")
	mustContain(t, all, "route", "frozen", "debug")
}

// TestQueuesSLURM drives `mu hpc queues --node sandslurm` — same stub, other cluster:
// the sbslurm queue_class override must relabel standard's class, and with no
// cores_per_node there the MaxNodes column is dropped entirely (no stray "32").
func TestQueuesSLURM(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queues", "--node", "sandslurm")
	mustContain(t, out, "standard", "bigmem", "17")
	if strings.Contains(out, "32") {
		t.Errorf("MaxNodes rendered without cores_per_node configured:\n%s", out)
	}
}

// TestStorage drives `mu hpc storage --node sandbox` through the box's show_storage
// stub: rows parse past the banner's own `=` divider, KB figures land as human sizes,
// and the derived Use% columns appear (50% home, 90% near-quota cwfs).
func TestStorage(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "storage", "--node", "sandbox")
	mustContain(t, out, "/p/home/tester", "50.0GB", "100.0GB", "50%", "90%", "95%")
	if strings.Contains(out, "SYSTEM") || strings.Contains(out, "hpc1") {
		t.Errorf("System column leaked into the single-cluster view:\n%s", out)
	}
}

// TestStorageFleet drives `mu hpc storage -f`: no fleet list is configured, so the scope
// falls back to one node per active cluster — both aliases hit the one box and the merged
// table carries a System column tagged with the CONFIG cluster names (not the site-
// reported "hpc1").
func TestStorageFleet(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "storage", "-f")
	mustContain(t, out, "SYSTEM", "sbpbs", "sbslurm", "/p/home/tester")
}

// TestInfoPBS drives `mu hpc queue info` (minfo) end-to-end on the PBS idiom: snapshot
// the queue (qstat -a), resolve the selector, fetch detail (qstat -f), render the house
// card. WorkDir proves the -f detail parsed, not just the snapshot row.
func TestInfoPBS(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "info", "--node", "sandbox", "1284570")
	mustContain(t, out, "1284570", "run_wave", "/home/tester/run")
}

// TestInfoSLURM drives minfo on the SLURM idiom (squeue snapshot → scontrol show job).
// Account is SLURM-detail-only, so it proves the scontrol output parsed.
func TestInfoSLURM(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "info", "--node", "sandslurm", "8359638")
	mustContain(t, out, "8359638", "run_wave", "proj123")
}

// TestHistPBS drives `mu hpc queue hist` (mhist) on the PBS idiom — the qstat -x stub's
// finished jobs render in the history table.
func TestHistPBS(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "hist", "--node", "sandbox")
	mustContain(t, out, "history", "done_run", "quick_test")
}

// TestHistSLURM drives mhist on the SLURM idiom — the sacct stub's pipe-delimited rows,
// including the FAILED one, render in the history table.
func TestHistSLURM(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "hist", "--node", "sandslurm")
	mustContain(t, out, "history", "done_run", "failed_run")
}

// muCfgLocal is muCfg without MU_SYSTEM=hpc — for verbs registered local-only (sshfs),
// which onHPC() would otherwise hide. Ticket safety holds without the env guard: the
// sandbox bin/ klist stub (first on PATH) reports a live ticket for tester, so
// EnsureTicket returns before pkinit — and the no-op pkinit stub backstops even that.
func muCfgLocal(t *testing.T, cfg string, args ...string) string {
	t.Helper()
	var env []string
	for _, kv := range muEnv() {
		switch {
		case strings.HasPrefix(kv, "MU_CONFIG_FILE="):
			kv = "MU_CONFIG_FILE=" + cfg
		case kv == "MU_SYSTEM=hpc":
			continue
		}
		env = append(env, kv)
	}
	cmd := exec.Command(muBin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mu %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestSSHFSRoundtrip drives the sshfs verbs end-to-end against the box: register a mount
// (add), mount it for real (fuse-t/sshfs on this machine → the box's sshd/sftp), read a
// box-side marker file THROUGH the mount, unmount, and confirm the marker is unreachable
// after — proving it was the fuse mount, not a stray local file. A temp [sshfs] root keeps
// the real registry/mounts tree untouched. Skips when sshfs isn't installed locally.
func TestSSHFSRoundtrip(t *testing.T) {
	requireSandbox(t)
	if _, err := exec.LookPath("sshfs"); err != nil {
		t.Skip("sshfs not installed here")
	}
	if out, err := exec.Command("ssh", "-q", "sandbox",
		"sh -c 'echo sshfs-marker-ok > sshfs_marker.txt'").CombinedOutput(); err != nil {
		t.Fatalf("seed marker on box: %v\n%s", err, out)
	}
	// EvalSymlinks: macOS TMPDIR is under /var → /private/var, but the mount table
	// lists resolved paths and IsMounted matches textually — hand mu the real path.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(os.Getenv("MU_SANDBOX_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte(string(base)+"\n[sshfs]\nroot = \""+root+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Best-effort teardown so a failed assertion never leaves a live fuse mount for
	// t.TempDir's cleanup to trip over (registered after TempDir → runs before it).
	mdir := filepath.Join(root, "mounts", "boxhome")
	t.Cleanup(func() { _ = exec.Command("umount", mdir).Run() })

	muCfgLocal(t, cfg, "sshfs", "add", "boxhome", "sandbox", "/home/tester")
	mustContain(t, muCfgLocal(t, cfg, "sshfs", "mount", "boxhome"), "mounted boxhome")
	got, err := os.ReadFile(filepath.Join(mdir, "sshfs_marker.txt"))
	if err != nil {
		t.Fatalf("read through mount: %v", err)
	}
	if strings.TrimSpace(string(got)) != "sshfs-marker-ok" {
		t.Errorf("marker mismatch: %q", got)
	}
	mustContain(t, muCfgLocal(t, cfg, "sshfs", "umount", "boxhome"), "unmounted boxhome")
	if _, err := os.ReadFile(filepath.Join(mdir, "sshfs_marker.txt")); err == nil {
		t.Error("marker still readable after umount — was it ever a fuse mount?")
	}
}

// writeStub drops an executable script into dir.
func writeStub(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestTicketFailureAborts simulates the offline failure that motivated the EnsureTicket
// rework: klist shows an EXPIRED ticket for tester (mere presence must not pass) and
// pkinit fails like an unreachable KDC. The mount must abort immediately with the pkinit
// error — not proceed into sshfs and bury the cause under a 30s "server unreachable"
// timeout. Runs without MU_SYSTEM=hpc (sshfs verbs are local-only), so Kerberos safety
// is carried entirely by these failing stubs sitting FIRST on PATH.
func TestTicketFailureAborts(t *testing.T) {
	requireSandbox(t)
	bin := t.TempDir()
	writeStub(t, bin, "klist", `cat <<'EOF'
Credentials cache: sandbox
Default principal: tester@SANDBOX.LOCAL

Valid Starting       Expires              Service Principal
01/01/2020 00:00:00  01/02/2020 00:00:00  krbtgt/SANDBOX.LOCAL@SANDBOX.LOCAL
EOF`)
	writeStub(t, bin, "pkinit", `echo "pkinit: unable to reach KDC" >&2; exit 1`)

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(os.Getenv("MU_SANDBOX_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte(string(base)+"\n[sshfs]\nroot = \""+root+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	muCfgLocal(t, cfg, "sshfs", "add", "boxhome", "sandbox", "/home/tester") // add never touches Kerberos

	var env []string
	for _, kv := range muEnv() {
		switch {
		case strings.HasPrefix(kv, "MU_CONFIG_FILE="):
			kv = "MU_CONFIG_FILE=" + cfg
		case kv == "MU_SYSTEM=hpc":
			continue
		case strings.HasPrefix(kv, "PATH="):
			kv = "PATH=" + bin + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
		}
		env = append(env, kv)
	}
	cmd := exec.Command(muBin, "sshfs", "mount", "boxhome")
	cmd.Env = env
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("mount succeeded despite a dead ticket:\n%s", out)
	}
	mustContain(t, string(out), "pkinit failed")
	if strings.Contains(string(out), "mounted boxhome") || strings.Contains(string(out), "timed out") {
		t.Errorf("mount proceeded past the ticket failure:\n%s", out)
	}
	// The whole point: fail in the ticket check, not after the 30s sshfs deadline.
	if e := time.Since(start); e > 15*time.Second {
		t.Errorf("abort took %s — did it wait out the sshfs spinner?", e)
	}
}

// repoRoot is the mayhl_utils checkout root, from the package dir (internal/integration).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestOnboard drives `mu setup onboard sandbox` end-to-end: cross-build a linux mu, push
// it + the tracked .config, and seed config.toml. Then verifies each landed on the box.
// The box is throwaway and onboard is idempotent, so re-running is safe.
func TestOnboard(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t))
	mustContain(t, out, "onboard complete")
	// The pushed mu binary runs on the box.
	if err := exec.Command("ssh", "sandbox", "~/.local/bin/mu --version").Run(); err != nil {
		t.Errorf("pushed mu not runnable on box: %v", err)
	}
	// .config landed as a live git repo (not a loose tar snapshot), so it stays
	// git-managed on the box (git pull to update).
	if got, _ := exec.Command("ssh", "-q", "sandbox", "git -C ~/.config rev-parse --is-inside-work-tree").CombinedOutput(); strings.TrimSpace(string(got)) != "true" {
		t.Errorf(".config is not a git work tree on box: %q", got)
	}
	// origin points at the public https remote (keyless pull on an egress box).
	if got, _ := exec.Command("ssh", "-q", "sandbox", "git -C ~/.config remote get-url origin").CombinedOutput(); !strings.HasPrefix(strings.TrimSpace(string(got)), "https://") {
		t.Errorf("origin not set to https remote: %q", got)
	}
	// A tracked file (checked out) and the untracked, machine-specific config.toml
	// (seeded, left intact by reset --hard) both present.
	for _, path := range []string{"~/.config/mu/config.toml", "~/.config/mise/config.toml"} {
		if err := exec.Command("ssh", "sandbox", "test -f "+path).Run(); err != nil {
			t.Errorf("expected %s on box: %v", path, err)
		}
	}
	// `mu doctor setup` introspects the freshly-onboarded box and reports each facet
	// (folded in from the former TestDoctorSetup — same onboarded box, no second onboard).
	// Warn-only, never fails, so it exits 0 even on a partially-set-up box.
	ds, err := exec.Command("ssh", "sandbox", "MU_RENDER=plain ~/.local/bin/mu doctor setup").CombinedOutput()
	if err != nil {
		t.Fatalf("doctor setup on box: %v\n%s", err, ds)
	}
	mustContain(t, string(ds), "shell-init", "toolchain", "build", "repo")
}

// TestSync checks `mu setup sync`: this machine's config.toml inventory propagates to the
// box while the box's machine-local [sshfs] seam survives, and a second run is a no-op.
func TestSync(t *testing.T) {
	requireSandbox(t)
	mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t))
	// Give the box a config.toml with a machine-local seam sync must preserve.
	boxConfig := "hpc_user = \"boxuser\"\n[sshfs]\nroot = \"/box/only/mnt\"\n"
	w := exec.Command("ssh", "sandbox", "cat > ~/.config/mu/config.toml")
	w.Stdin = strings.NewReader(boxConfig)
	if err := w.Run(); err != nil {
		t.Fatalf("seed box config: %v", err)
	}
	// Sync the laptop's test-config.toml inventory over.
	mu(t, "setup", "sync", "sandbox", "-y")
	got, err := exec.Command("ssh", "sandbox", "cat ~/.config/mu/config.toml").CombinedOutput()
	if err != nil {
		t.Fatalf("read box config: %v\n%s", err, got)
	}
	s := string(got)
	if !strings.Contains(s, "sbpbs") {
		t.Errorf("inventory not synced (no sbpbs cluster):\n%s", s)
	}
	if !strings.Contains(s, "/box/only/mnt") {
		t.Errorf("target [sshfs] seam was clobbered:\n%s", s)
	}
	// Idempotent: nothing changed since, so the second run is a no-op.
	out := mu(t, "setup", "sync", "sandbox", "-y")
	mustContain(t, out, "already in sync")
}

// TestSyncPull checks `mu setup sync pull`: a box's config.toml inventory comes INTO this
// machine's config.toml, this machine's [sshfs] seam survives, the box's seam does not, a
// config.toml.bak backup is written, and a second run is a no-op. Writes to a temp config so
// the shared fixture is never touched.
func TestSyncPull(t *testing.T) {
	requireSandbox(t)
	mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t))

	// Box config: the shared inventory (so `sandbox` still resolves after the pull) plus a
	// distinct marker cluster to prove inventory flowed, and a box-only [sshfs] seam to DROP.
	boxConfig := "hpc_user = \"tester\"\n\n" +
		"[[cluster]]\nname = \"sbpbs\"\ndomain = \"local\"\nnodes = [\"sandbox\"]\nscheduler = \"pbs\"\n\n" +
		"[[cluster]]\nname = \"sbslurm\"\ndomain = \"local\"\nnodes = [\"sandslurm\"]\nscheduler = \"slurm\"\n\n" +
		"[[cluster]]\nname = \"pulledclust\"\ndomain = \"local\"\nnodes = [\"boxnode\"]\nscheduler = \"pbs\"\n\n" +
		"[sshfs]\nroot = \"/box/only/mnt\"\n"
	w := exec.Command("ssh", "sandbox", "cat > ~/.config/mu/config.toml")
	w.Stdin = strings.NewReader(boxConfig)
	if err := w.Run(); err != nil {
		t.Fatalf("seed box config: %v", err)
	}

	// Local config: the fixture inventory (resolves `sandbox`) plus a laptop-only [sshfs] seam
	// pull must KEEP. In a temp file so the real fixture is never mutated.
	base, err := os.ReadFile(os.Getenv("MU_SANDBOX_CONFIG"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte(string(base)+"\n[sshfs]\nroot = \"/laptop/only/mnt\"\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	muCfg(t, cfg, "setup", "sync", "pull", "sandbox", "-y")

	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read pulled config: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "pulledclust") {
		t.Errorf("box inventory not pulled (no pulledclust cluster):\n%s", s)
	}
	if !strings.Contains(s, "/laptop/only/mnt") {
		t.Errorf("local [sshfs] seam was clobbered:\n%s", s)
	}
	if strings.Contains(s, "/box/only/mnt") {
		t.Errorf("box [sshfs] seam leaked into local config:\n%s", s)
	}
	if _, err := os.Stat(cfg + ".bak"); err != nil {
		t.Errorf("no config.toml.bak backup written: %v", err)
	}
	// Idempotent: nothing changed, so the second pull is a no-op.
	out := muCfg(t, cfg, "setup", "sync", "pull", "sandbox", "-y")
	mustContain(t, out, "already in sync")
}

// TestSyncPullDotfiles checks `mu setup sync pull --dotfiles`: the .config git repo is
// reconciled box → this machine (fetch + backup ref + fast-forward). It works on a local
// CLONE of the box's .config (via --config-dir) so the real ~/.config is never touched, and
// rewinds that clone one commit so the box is one ahead — the pull must FF it back up.
func TestSyncPullDotfiles(t *testing.T) {
	requireSandbox(t)
	mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t))
	const boxTarget = "tester@sandbox.local" // hpc_user=tester, node=sandbox, domain=local

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "config")
	if out, err := exec.Command("git", "clone", "-q", boxTarget+":.config", dir).CombinedOutput(); err != nil {
		t.Fatalf("clone box .config: %v\n%s", err, out)
	}
	boxHead := gitOut(t, dir, "rev-parse", "HEAD")
	if _, err := exec.Command("git", "-C", dir, "reset", "--hard", "-q", "HEAD~1").CombinedOutput(); err != nil {
		t.Fatalf("rewind clone: %v", err)
	}
	oldHead := gitOut(t, dir, "rev-parse", "HEAD")
	if boxHead == oldHead {
		t.Fatal("rewind was a no-op")
	}

	// Temp config.toml so the config.toml half of the pull doesn't touch the fixture.
	base, err := os.ReadFile(os.Getenv("MU_SANDBOX_CONFIG"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	cfg := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfg, base, 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	muCfg(t, cfg, "setup", "sync", "pull", "sandbox", "--dotfiles", "--config-dir", dir, "-y")

	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != boxHead {
		t.Errorf(".config not fast-forwarded to box HEAD: got %s want %s", got, boxHead)
	}
	if got := gitOut(t, dir, "rev-parse", "mu-sync-backup"); got != oldHead {
		t.Errorf("backup ref not at pre-merge HEAD: got %s want %s", got, oldHead)
	}
	// Idempotent: nothing new on the box, so a second --dotfiles pull is up to date.
	out := muCfg(t, cfg, "setup", "sync", "pull", "sandbox", "--dotfiles", "--config-dir", dir, "-y")
	mustContain(t, out, "already up to date")
}

// TestShellLayerOnBox proves the binary is self-sufficient on a real box: after onboard
// (mu binary + .config, NO mayhl_utils source, no init.sh), eval'ing `mu setup --eval zsh`
// on the box defines the full functional shell layer — the connectivity seam
// (mu_ssh_login/mu_auth), support libs, shared tooling, and front-doors. This is the
// contract .zshrc.hpc relies on for a no-checkout HPC login node.
func TestShellLayerOnBox(t *testing.T) {
	requireSandbox(t)
	mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t))
	// Eval the wire on the box (as MU_SYSTEM=hpc → the HPC seam) and report each helper.
	script := `export MU_SYSTEM=hpc
export PATH="$HOME/.local/bin:$PATH"
eval "$(mu setup --eval zsh 2>/dev/null)"
for f in mu_log mu_ssh_login mu_auth qtar mu_status gkill mu_run mps mlog; do
  command -v "$f" >/dev/null 2>&1 && echo "HAVE $f" || echo "MISS $f"
done`
	cmd := exec.Command("ssh", "-q", "sandbox", "zsh -s")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("box shell-layer eval: %v\n%s", err, out)
	}
	got := string(out)
	if strings.Contains(got, "MISS ") {
		t.Errorf("binary not self-sufficient on the box (a helper was undefined):\n%s", got)
	}
	for _, w := range []string{"mu_ssh_login", "mu_auth", "qtar", "mu_status", "gkill", "mps", "mlog"} {
		if !strings.Contains(got, "HAVE "+w) {
			t.Errorf("missing HAVE %s on the box:\n%s", w, got)
		}
	}
}

// TestSSHBannerQuieted checks mu's ssh calls pass -q so the box's login banner never leaks
// into mu's output. Baseline: a raw ssh shows the mock banner; a sync push (which forwards
// ssh stderr via pipeSSH) must not. Skips if the box serves no banner (pre-banner image).
func TestSSHBannerQuieted(t *testing.T) {
	requireSandbox(t)
	mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t))
	const mark = "MU-MOCK-BANNER"
	raw, _ := exec.Command("ssh", "tester@sandbox.local", "true").CombinedOutput()
	if !strings.Contains(string(raw), mark) {
		t.Skip("box serves no banner (rebuild the sandbox image with the mock banner)")
	}
	// Give the box a differing config.toml so `sync` actually writes via pipeSSH — the
	// stderr-forwarding path that would leak the banner without -q.
	w := exec.Command("ssh", "sandbox", "cat > ~/.config/mu/config.toml")
	w.Stdin = strings.NewReader("hpc_user = \"boxuser\"\n[sshfs]\nroot = \"/box/only/mnt\"\n")
	if err := w.Run(); err != nil {
		t.Fatalf("seed box config: %v", err)
	}
	out := mu(t, "setup", "sync", "sandbox", "-y")
	if strings.Contains(out, mark) {
		t.Errorf("ssh banner leaked into mu output (missing -q?):\n%s", out)
	}
}

// TestOnboardDirtyGuard checks the reset --hard guard: a tracked file edited on the box
// must survive a plain re-onboard (skipped with a warning), and only --force overwrites it
// — after backing the work up to branch mu-onboard-backup + a stash.
func TestOnboardDirtyGuard(t *testing.T) {
	requireSandbox(t)
	const sentinel = "MU-LOCAL-EDIT-KEEP"
	// Start from a clean synced .config, then dirty a tracked file on the box.
	mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t))
	if err := exec.Command("ssh", "sandbox", "echo "+sentinel+" >> ~/.config/mise/config.toml").Run(); err != nil {
		t.Fatalf("dirty the box file: %v", err)
	}
	// Plain re-onboard: must NOT clobber the local edit.
	out := mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t))
	mustContain(t, out, "skipped .config sync")
	if err := exec.Command("ssh", "sandbox", "grep -q "+sentinel+" ~/.config/mise/config.toml").Run(); err != nil {
		t.Errorf("local edit was lost by a non-force onboard: %v", err)
	}
	// --force: overwrites the edit but backs it up to branch mu-onboard-backup.
	mu(t, "setup", "onboard", "sandbox", "--repo", repoRoot(t), "--force")
	if err := exec.Command("ssh", "sandbox", "grep -q "+sentinel+" ~/.config/mise/config.toml").Run(); err == nil {
		t.Error("--force did not overwrite the local edit")
	}
	if err := exec.Command("ssh", "sandbox", "git -C ~/.config rev-parse --verify mu-onboard-backup").Run(); err != nil {
		t.Errorf("--force did not create the backup branch: %v", err)
	}
}

// TestKillSLURM proves the real scancel stub accepts mu's cancel command over ssh — the
// SLURM idiom (distinct binary + KillCmd from qdel). Id 8359638 is already bare, so no
// selector logic is under test here; the value is the scancel wiring.
func TestKillSLURM(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "kill", "--node", "sandslurm", "-y", "8359638")
	mustContain(t, out, "cancelled 1 job(s) on sandslurm")
}

// TestKillRange proves the real qdel stub accepts a BATCHED multi-id cancel in one
// invocation (1284570-1284571 → run_wave + post_proc). The range→2-jobs selection itself
// is unit-tested (queue.TestMatchRangeAndList); this asserts the batch wiring.
func TestKillRange(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "kill", "--node", "sandbox", "-y", "1284570-1284571")
	mustContain(t, out, "cancelled 2 job(s) on sandbox")
}

// TestKillNoMatch proves an empty selection runs NO scheduler command and exits cleanly
// (0) with a notice — the wiring the empty-match guard can't show at the unit level (it
// sits behind the ssh-backed queueTargetCtx). The selector match itself is unit-tested.
func TestKillNoMatch(t *testing.T) {
	requireSandbox(t)
	out := mu(t, "hpc", "queue", "kill", "--node", "sandbox", "-y", "9999999")
	mustContain(t, out, "no matching jobs on sandbox")
}

// TestCPRoundtrip pushes a file to the box with `mu cp push` and pulls it back with
// `mu cp pull`, asserting the contents survive the round trip.
func TestCPRoundtrip(t *testing.T) {
	requireSandbox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.txt")
	const body = "sandbox-cp-ok\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mu(t, "cp", "push", "sandbox", src, "cp_test.txt")
	back := filepath.Join(dir, "back.txt")
	mu(t, "cp", "pull", "sandbox", "cp_test.txt", back)
	got, err := os.ReadFile(back)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != body {
		t.Errorf("roundtrip mismatch: got %q want %q", got, body)
	}
}
