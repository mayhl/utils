package cli

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEstimateCoreHours(t *testing.T) {
	// PBS: cores/node from the select chunk (ncpus), nodes from select.
	// 12:00:00 × 4 nodes × 128 cores = 6144 core-hours.
	pbs := writeScript(t, "#!/bin/bash\n#PBS -l walltime=12:00:00\n#PBS -l select=4:ncpus=128:mpiprocs=128\n")
	if h, basis, ok := estimateCoreHours(pbs, "unknown-node"); !ok || math.Abs(h-6144) > 1e-6 {
		t.Errorf("pbs: h=%v basis=%q ok=%v, want 6144", h, basis, ok)
	}

	// SLURM: nodes from --nodes, cores from --ntasks-per-node.
	// 2:00:00 × 2 nodes × 48 cores = 192 core-hours.
	slurm := writeScript(t, "#!/bin/bash\n#SBATCH --time=2:00:00\n#SBATCH --nodes=2\n#SBATCH --ntasks-per-node=48\n")
	if h, _, ok := estimateCoreHours(slurm, "unknown-node"); !ok || math.Abs(h-192) > 1e-6 {
		t.Errorf("slurm: h=%v ok=%v, want 192", h, ok)
	}

	// A node count absent → the soft default of 1. 1:00:00 × 1 × 16 = 16.
	oneNode := writeScript(t, "#PBS -l walltime=1:00:00\n#PBS -l select=1:ncpus=16\n")
	if h, _, ok := estimateCoreHours(oneNode, "unknown-node"); !ok || math.Abs(h-16) > 1e-6 {
		t.Errorf("one-node: h=%v ok=%v, want 16", h, ok)
	}

	// No walltime → can't estimate; the caller leans on --hours.
	noWall := writeScript(t, "#!/bin/bash\n#PBS -l select=4:ncpus=128\n")
	if _, _, ok := estimateCoreHours(noWall, "unknown-node"); ok {
		t.Error("no-walltime: want ok=false")
	}

	// Walltime but cores/node neither declared nor configured → can't estimate.
	noCores := writeScript(t, "#SBATCH --time=1:00:00\n#SBATCH --nodes=2\n")
	if _, _, ok := estimateCoreHours(noCores, "unknown-node"); ok {
		t.Error("no-cores: want ok=false")
	}

	// An unreadable (remote) script → ok=false, no panic.
	if _, _, ok := estimateCoreHours("/no/such/script.sh", "unknown-node"); ok {
		t.Error("missing script: want ok=false")
	}
}
