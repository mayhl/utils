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
		return errors.New("MU_SANDBOX_CONFIG unset")
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

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in output:\n%s", w, out)
		}
	}
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
