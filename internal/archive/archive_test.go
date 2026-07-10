package archive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// world builds a scratch-tier batch: a parent with two small run leaves, a
// staged case copy, and a stray file. Returns the work root and the parent.
func world(t *testing.T) (work, parent string) {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	work = filepath.Join(base, "work")
	parent = filepath.Join(work, "proj/simulations/funwave")
	for _, p := range []string{
		filepath.Join(home, "proj/simulations/funwave/case_a"),
		filepath.Join(parent, "case_a"),
		filepath.Join(parent, "case_a_100"),
		filepath.Join(parent, "case_a_250"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		filepath.Join(home, "proj/simulations/funwave/case_a/input.nml"),
		filepath.Join(parent, "case_a_100/out.nc"),
		filepath.Join(parent, "case_a_250/out.nc"),
		filepath.Join(parent, "notes.txt"),
	} {
		if err := os.WriteFile(f, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("WORKDIR", work)
	t.Setenv("ARCHIVE_HOME", "/arch")
	t.Setenv("MU_CONFIG_FILE", filepath.Join(base, "nonexistent.toml"))
	t.Setenv("MU_ROOT", "")
	config.ResetForTest()
	return work, parent
}

func TestPlanDirLeaf(t *testing.T) {
	_, parent := world(t)
	ps, rc := planDir(filepath.Join(parent, "case_a_250"))
	if rc != 0 || len(ps) != 1 {
		t.Fatalf("rc=%d packs=%v", rc, ps)
	}
	want := pack{
		dir:  filepath.Join(parent, "case_a_250"),
		dst:  "/arch/proj/simulations/funwave/case_a",
		name: "250.tar",
	}
	if ps[0] != want {
		t.Fatalf("got %+v want %+v", ps[0], want)
	}
}

func TestPlanDirBatch(t *testing.T) {
	_, parent := world(t)
	// tiny leaves, default 1GB threshold → the whole parent packs as ONE tar
	ps, rc := planDir(parent)
	if rc != 0 || len(ps) != 1 {
		t.Fatalf("rc=%d packs=%v", rc, ps)
	}
	want := pack{dir: parent, dst: "/arch/proj/simulations", name: "funwave.tar"}
	if ps[0] != want {
		t.Fatalf("got %+v want %+v", ps[0], want)
	}
}

func TestPlanDirMixedFallsToLeaves(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.toml")
	if err := os.WriteFile(cfgFile, []byte("[project]\ntar_parent_threshold = \"1B\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, parent := world(t)
	t.Setenv("MU_CONFIG_FILE", cfgFile)
	config.ResetForTest()
	// every leaf is >= 1B → all-or-nothing tips the batch to per-leaf tars; the
	// staged bare case violates the inputs-from-permanent guard and is skipped
	ps, rc := planDir(parent)
	if rc != 0 || len(ps) != 2 {
		t.Fatalf("rc=%d packs=%v", rc, ps)
	}
	for i, name := range []string{"100.tar", "250.tar"} {
		if ps[i].name != name || ps[i].dst != "/arch/proj/simulations/funwave/case_a" {
			t.Fatalf("pack %d: %+v", i, ps[i])
		}
	}
}

func TestPlanDirLeafGuardAborts(t *testing.T) {
	_, parent := world(t)
	// the staged bare case named EXPLICITLY (not swept up in a batch) is a hard
	// error — inputs archive from the permanent tier
	ps, rc := planDir(filepath.Join(parent, "case_a"))
	if rc == 0 || ps != nil {
		t.Fatalf("expected the guard to abort, got rc=%d packs=%v", rc, ps)
	}
}

func TestPlanDirNonCasePassesThrough(t *testing.T) {
	work, _ := world(t)
	dir := filepath.Join(work, "proj/scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ps, rc := planDir(dir)
	if rc != 0 || ps != nil {
		t.Fatalf("expected passthrough, got rc=%d packs=%v", rc, ps)
	}
}

func TestFlagAndCDetection(t *testing.T) {
	if !hasArg([]string{"put", "-C", "/x"}, "-C") || hasArg([]string{"-Cx"}, "-C") {
		t.Fatal("hasArg")
	}
	if !hasFlags([]string{"-retry", "5", "f"}) || hasFlags([]string{"file", "dir/"}) {
		t.Fatal("hasFlags")
	}
}
