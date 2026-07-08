package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearJobEnv blanks the scheduler-detect vars so a test controls which scheduler is
// "active" regardless of the runner's environment (t.Setenv restores on cleanup).
func clearJobEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{"PBS_JOBID", "SLURM_JOB_ID", "SLURM_JOB_NODELIST", "PBS_NODEFILE", "TMPDIR"} {
		t.Setenv(v, "")
	}
}

func TestEnvPBS(t *testing.T) {
	clearJobEnv(t)
	t.Setenv("PBS_JOBID", "12345.pbs")
	t.Setenv("PBS_JOBNAME", "run_wave")
	t.Setenv("PBS_O_WORKDIR", "/p/work/me/run 1") // a space → must be quoted
	t.Setenv("PBS_NUM_NODES", "4")
	t.Setenv("PBS_NODEFILE", "/var/spool/pbs/aux/12345")

	out, err := Env()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export MU_SCHEDULER=pbs",
		"export MU_JOBID='12345.pbs'",
		"export MU_JOBNAME='run_wave'",
		"export MU_SUBMIT_DIR='/p/work/me/run 1'",
		"export MU_NUM_NODES='4'",
		"export MU_NODEFILE='/var/spool/pbs/aux/12345'", // PBS: passthrough
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "MU_NTASKS") {
		t.Errorf("PBS should not emit the SLURM-only MU_NTASKS:\n%s", out)
	}
}

func TestEnvSLURMNodefile(t *testing.T) {
	clearJobEnv(t)
	// Stub scontrol on PATH: expand any nodelist to two hostnames.
	dir := t.TempDir()
	sc := filepath.Join(dir, "scontrol")
	if err := os.WriteFile(sc, []byte("#!/bin/sh\nprintf 'node01\\nnode02\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", dir)
	t.Setenv("SLURM_JOB_ID", "999")
	t.Setenv("SLURM_JOB_NAME", "mesh_gen")
	t.Setenv("SLURM_NTASKS", "128")
	t.Setenv("SLURM_JOB_NODELIST", "node[01-02]")

	out, err := Env()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "export MU_SCHEDULER=slurm") ||
		!strings.Contains(out, "export MU_JOBID='999'") ||
		!strings.Contains(out, "export MU_NTASKS='128'") {
		t.Errorf("missing base SLURM vars:\n%s", out)
	}
	// NODEFILE: the compressed nodelist must be expanded to a real file.
	nfPath := filepath.Join(dir, "mu-nodefile-999")
	if !strings.Contains(out, "export MU_NODEFILE='"+nfPath+"'") {
		t.Errorf("MU_NODEFILE not emitted as %s:\n%s", nfPath, out)
	}
	if b, _ := os.ReadFile(nfPath); string(b) != "node01\nnode02\n" {
		t.Errorf("expanded nodefile = %q, want the two hostnames", b)
	}
}

func TestEnvNoJob(t *testing.T) {
	clearJobEnv(t)
	if _, err := Env(); err == nil {
		t.Error("expected an error outside a job, got nil")
	}
}
