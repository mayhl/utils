package mirror

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// world builds a two-tier project tree: authored cases + shared data on "home",
// staged copy, runs, and shared data on "work". Returns the two roots.
func world(t *testing.T) (home, work string) {
	t.Helper()
	base := t.TempDir()
	home, work = filepath.Join(base, "home"), filepath.Join(base, "work")
	for _, p := range []string{
		"home/proj/simulations/funwave/case_a",
		"home/proj/simulations/funwave/case_b",
		"home/proj/simulations/data",
		"home/proj/scripts",
		"work/proj/simulations/funwave/case_a",
		"work/proj/simulations/funwave/case_a_100",
		"work/proj/simulations/funwave/case_a_250",
		"work/proj/simulations/data",
		"work/proj/scripts",
	} {
		if err := os.MkdirAll(filepath.Join(base, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"home/proj/simulations/funwave/case_a/input.nml",
		"work/proj/simulations/funwave/case_a_250/input.nml",
		"work/proj/simulations/funwave/case_a_250/out.nc",
		"work/proj/simulations/data/bathy.dep",
	} {
		if err := os.WriteFile(filepath.Join(base, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// case_a_250 is the newer run.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(work, "proj/simulations/funwave/case_a_100"), old, old); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("WORKDIR", work)
	t.Setenv("ARCHIVE_HOME", "/arch")
	t.Setenv("MU_CONFIG_FILE", filepath.Join(base, "nonexistent.toml"))
	t.Setenv("MU_ROOT", "")
	config.ResetForTest()
	return home, work
}

func TestSwap(t *testing.T) {
	home, work := world(t)
	sim := "proj/simulations/funwave"

	cases := []struct {
		name, in, want string
	}{
		{"run dir → case dir", filepath.Join(work, sim, "case_a_100"), filepath.Join(home, sim, "case_a")},
		{"file in run → file in case", filepath.Join(work, sim, "case_a_250/input.nml"), filepath.Join(home, sim, "case_a/input.nml")},
		{"case dir → NEWEST run", filepath.Join(home, sim, "case_a"), filepath.Join(work, sim, "case_a_250")},
		{"file in case → file in newest run", filepath.Join(home, sim, "case_a/input.nml"), filepath.Join(work, sim, "case_a_250/input.nml")},
		{"no runs → bare staged copy is missing → see error case below", "", ""},
		{"plain dir toggles", filepath.Join(home, "proj/scripts"), filepath.Join(work, "proj/scripts")},
	}
	for _, c := range cases {
		if c.in == "" {
			continue
		}
		got, err := Swap(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}

	// case_b has no runs and no staged copy on work → error.
	if _, err := Swap(filepath.Join(home, sim, "case_b")); err == nil {
		t.Error("case with no runs/staged copy: want error")
	}
	// Nonexistent counterpart → error, not a blind path.
	if _, err := Swap(filepath.Join(home, "proj/simulations/data")); err != nil {
		t.Errorf("existing counterpart dir: %v", err)
	}
	if _, err := Swap("/outside/everything"); err == nil {
		t.Error("path outside all sets: want error")
	}
}

func TestSwapNoScratchTier(t *testing.T) {
	world(t)
	t.Setenv("WORKDIR", "")
	if _, err := Swap(filepath.Join(os.Getenv("HOME"), "proj/scripts")); err == nil {
		t.Error("no $WORKDIR: want no-swap-tier error")
	}
}

func TestArchive(t *testing.T) {
	home, work := world(t)
	sim := "proj/simulations/funwave"

	cases := []struct {
		name, in, want string
	}{
		{"case input → input nest", filepath.Join(home, sim, "case_a"), "/arch/" + sim + "/case_a/input"},
		{"file in case input", filepath.Join(home, sim, "case_a/input.nml"), "/arch/" + sim + "/case_a/input/input.nml"},
		{"run → jobid nest", filepath.Join(work, sim, "case_a_100"), "/arch/" + sim + "/case_a/100"},
		{"file in run (pattern-anchored pivot)", filepath.Join(work, sim, "case_a_250/out.nc"), "/arch/" + sim + "/case_a/250/out.nc"},
		{"array-run suffix", filepath.Join(work, sim, "case_a_100-3"), "/arch/" + sim + "/case_a/100-3"},
		{"shared data from scratch 1:1", filepath.Join(work, "proj/simulations/data/bathy.dep"), "/arch/proj/simulations/data/bathy.dep"},
		{"plain from home 1:1", filepath.Join(home, "proj/scripts"), "/arch/proj/scripts"},
	}
	for _, c := range cases {
		got, err := Archive(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}

	// Provenance guards: each class only from its authoritative tier.
	for name, p := range map[string]string{
		"shared data from home": filepath.Join(home, "proj/simulations/data/bathy.dep"),
		"case input from work":  filepath.Join(work, sim, "case_a"),
	} {
		if _, err := Archive(p); err == nil {
			t.Errorf("%s: want refusal", name)
		}
	}

	t.Setenv("ARCHIVE_HOME", "")
	if _, err := Archive(filepath.Join(home, "proj/scripts")); err == nil {
		t.Error("no $ARCHIVE_HOME: want error")
	}
}

// TestNestedGroupSet proves longest-prefix precedence and single-root semantics:
// a group share nested under $HOME beats the default pair, archives 1:1 under its
// archive_rel with NO case transform, and refuses swap.
func TestNestedGroupSet(t *testing.T) {
	home, _ := world(t)
	share := filepath.Join(home, "projects/shared")
	if err := os.MkdirAll(filepath.Join(share, "case_x_99"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("[[mirror_set]]\nname = \"group\"\nroots = [%q]\narchive_rel = \"group/shared\"\n", share)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", cfg)
	config.ResetForTest()

	got, err := Archive(filepath.Join(share, "case_x_99"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/arch/group/shared/case_x_99"; got != want {
		t.Errorf("single-root archive: got %s, want %s (no case transform, under archive_rel)", got, want)
	}
	if _, err := Swap(filepath.Join(share, "case_x_99")); err == nil {
		t.Error("swap inside a single-root set: want no-swap-tier error")
	}
	// Outside the group set, the default pair still resolves.
	if _, err := Archive(filepath.Join(home, "proj/scripts")); err != nil {
		t.Errorf("default set still active: %v", err)
	}
}
