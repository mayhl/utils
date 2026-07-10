package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo builds a git project with a nested case dir; returns (root, caseDir).
func repo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	caseDir := filepath.Join(root, "simulations", "funwave", "case_a")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "input.nml"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	git(t, root, "config", "user.email", "test@example.com")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "commit.gpgsign", "false")
	return root, caseDir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestFindRoot(t *testing.T) {
	root, caseDir := repo(t)
	got, err := FindRoot(caseDir)
	if err != nil {
		t.Fatal(err)
	}
	// macOS TempDir is a /var → /private/var symlink; compare resolved paths.
	want, _ := filepath.EvalSymlinks(root)
	gotR, _ := filepath.EvalSymlinks(got)
	if gotR != want {
		t.Errorf("root = %s, want %s", gotR, want)
	}
	if _, err := FindRoot(t.TempDir()); err == nil {
		t.Error("no error outside a project")
	}
}

func TestHomeRel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := filepath.Join(home, "projects", "proj_a", "simulations", "case_a")
	rel, err := HomeRel(p)
	if err != nil {
		t.Fatal(err)
	}
	if rel != "projects/proj_a/simulations/case_a" {
		t.Errorf("rel = %s", rel)
	}
	if _, err := HomeRel("/somewhere/else"); err == nil {
		t.Error("no error outside $HOME")
	}
	if _, err := HomeRel(filepath.Dir(home)); err == nil {
		t.Error("no error for $HOME's parent")
	}
}

func TestNewStampNoCommit(t *testing.T) {
	_, caseDir := repo(t)
	s := NewStamp(caseDir)
	if s.Commit != "" {
		t.Errorf("commit = %q before any commit", s.Commit)
	}
	if !s.Dirty {
		t.Error("no-sha stamp must record dirty")
	}
	if s.Case != "simulations/funwave/case_a" {
		t.Errorf("case = %q", s.Case)
	}
}

func TestNewStampCleanAndDirty(t *testing.T) {
	root, caseDir := repo(t)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "init")

	s := NewStamp(caseDir)
	if len(s.Commit) != 40 {
		t.Errorf("commit = %q", s.Commit)
	}
	if s.Dirty {
		t.Error("clean tree reported dirty")
	}

	// Dirty is case-scoped: edits OUTSIDE the case dir must not taint it.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if NewStamp(caseDir).Dirty {
		t.Error("sibling edit tainted the case stamp")
	}
	if err := os.WriteFile(filepath.Join(caseDir, "input.nml"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !NewStamp(caseDir).Dirty {
		t.Error("case edit not reported dirty")
	}
}

func TestStampRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := Stamp{Case: "simulations/funwave/case_a", Commit: strings.Repeat("a", 40), Dirty: true}
	if err := os.WriteFile(filepath.Join(dir, StampFile), []byte(in.TOML()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, ok := ReadStamp(dir)
	if !ok {
		t.Fatal("ReadStamp failed")
	}
	if out != in {
		t.Errorf("round trip: %+v != %+v", out, in)
	}
	if _, ok := ReadStamp(t.TempDir()); ok {
		t.Error("ok for a dir without a stamp")
	}
}
