package job

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// caseDir builds a submit dir shaped like a case: inputs, a subdir, an executable
// script, and scheduler log droppings from prior runs (which must NOT be copied).
func caseDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "case_a")
	for p, mode := range map[string]os.FileMode{
		"input.nml":        0o644,
		"sub/bathy.ref":    0o644,
		"run.sh":           0o755,
		"case_a.o11111":    0o644, // PBS stdout, old run
		"case_a.e11111":    0o644, // PBS stderr, old run
		"slurm-99.out":     0o644, // SLURM, old run
		"slurm-99_2.out":   0o644, // SLURM array form
		"notes.out":        0o644, // NOT a scheduler log — must copy
		"model.o.settings": 0o644, // dot-o but no digits — must copy
	} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(p+"\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func pbsEnv(t *testing.T, submitDir string) {
	t.Helper()
	for _, v := range []string{"MU_JOBID", "MU_SUBMIT_DIR", "MU_ARRAY_INDEX", "MU_SCHEDULER", "MU_QUEUE", "SLURM_JOB_ID", "SLURM_SUBMIT_DIR", "SLURM_ARRAY_TASK_ID", "SLURM_JOB_PARTITION", "PBS_ARRAY_INDEX", "BC_HOST"} {
		t.Setenv(v, "")
	}
	t.Setenv("PBS_JOBID", "12345.pbsserver")
	t.Setenv("PBS_O_WORKDIR", submitDir)
	t.Setenv("PBS_QUEUE", "standard")
	t.Setenv("BC_HOST", "hpc1")
}

func TestPrepFresh(t *testing.T) {
	src := caseDir(t)
	pbsEnv(t, src)

	snippet, reused, err := Prep()
	if err != nil {
		t.Fatal(err)
	}
	if reused {
		t.Error("fresh prep reported reused")
	}
	runDir := src + "_12345"
	for _, w := range []string{"export MU_RUN_DIR='" + runDir + "'", `cd "$MU_RUN_DIR" || exit 1`} {
		if !strings.Contains(snippet, w) {
			t.Errorf("snippet missing %q:\n%s", w, snippet)
		}
	}

	for _, p := range []string{"input.nml", "sub/bathy.ref", "run.sh", "notes.out", "model.o.settings", "run.toml"} {
		if _, err := os.Stat(filepath.Join(runDir, p)); err != nil {
			t.Errorf("missing in run dir: %s", p)
		}
	}
	for _, p := range []string{"case_a.o11111", "case_a.e11111", "slurm-99.out", "slurm-99_2.out"} {
		if _, err := os.Stat(filepath.Join(runDir, p)); err == nil {
			t.Errorf("scheduler log copied into run dir: %s", p)
		}
	}
	if fi, err := os.Stat(filepath.Join(runDir, "run.sh")); err != nil || fi.Mode().Perm() != 0o755 {
		t.Error("exec bit not preserved on run.sh")
	}

	toml, err := os.ReadFile(filepath.Join(runDir, "run.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{`jobid = "12345.pbsserver"`, `scheduler = "pbs"`, `cluster = "hpc1"`, `queue = "standard"`, "started = "} {
		if !strings.Contains(string(toml), w) {
			t.Errorf("run.toml missing %q:\n%s", w, toml)
		}
	}
}

// TestPrepReuse proves the requeue contract: outputs in the run dir survive, and
// changed case inputs are NOT re-copied over the as-run snapshot.
func TestPrepReuse(t *testing.T) {
	src := caseDir(t)
	pbsEnv(t, src)
	if _, _, err := Prep(); err != nil {
		t.Fatal(err)
	}
	runDir := src + "_12345"
	if err := os.WriteFile(filepath.Join(runDir, "partial.dat"), []byte("output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "input.nml"), []byte("EDITED AFTER SUBMIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, reused, err := Prep()
	if err != nil {
		t.Fatal(err)
	}
	if !reused {
		t.Error("second prep did not report reuse")
	}
	if _, err := os.Stat(filepath.Join(runDir, "partial.dat")); err != nil {
		t.Error("requeue clobbered run output")
	}
	got, _ := os.ReadFile(filepath.Join(runDir, "input.nml"))
	if strings.Contains(string(got), "EDITED") {
		t.Error("requeue re-copied inputs over the as-run snapshot")
	}
}

func TestPrepArrayIndex(t *testing.T) {
	src := caseDir(t)
	pbsEnv(t, src)
	t.Setenv("PBS_ARRAY_INDEX", "7")
	if _, _, err := Prep(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src + "_12345-7"); err != nil {
		t.Error("array subjob did not get its own -<index> run dir")
	}
}

func TestRunDirNoSideEffects(t *testing.T) {
	src := caseDir(t)
	pbsEnv(t, src)
	dir, err := RunDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != src+"_12345" {
		t.Errorf("RunDir = %q, want %q", dir, src+"_12345")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("RunDir created the dir")
	}
}

func TestPrepOutsideJob(t *testing.T) {
	pbsEnv(t, t.TempDir())
	t.Setenv("PBS_JOBID", "")
	if _, _, err := Prep(); err == nil {
		t.Error("prep outside a job: want error")
	}
}

// TestRunTOMLGit covers the provenance git fields: case rel-path, commit, and the
// case-scoped dirty flag (clean → false, uncommitted case edit → true).
func TestRunTOMLGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	src := filepath.Join(repo, "simulations", "funwave", "case_a")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "input.nml"), []byte("dt=0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "case")
	pbsEnv(t, src)

	toml := runTOML("12345", src)
	for _, w := range []string{`case = "simulations/funwave/case_a"`, "commit = ", "dirty = false"} {
		if !strings.Contains(toml, w) {
			t.Errorf("run.toml missing %q:\n%s", w, toml)
		}
	}

	if err := os.WriteFile(filepath.Join(src, "input.nml"), []byte("dt=0.05\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runTOML("12345", src), "dirty = true") {
		t.Error("uncommitted case edit not flagged dirty")
	}
}
