// Package project implements the project-layer verbs over the mirror-namespace
// contract: locating the project root (the git repo), naming paths in the shared
// $HOME-relative namespace, and the submit-origin stamp that iterate-mode submits
// ship to the cluster ($WORK staging is not a checkout — the stamp is the only
// carrier of the inputs' commit).
package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// StampFile is the submit-origin record `mu project submit` drops in $WORK
// staging; `mu job prep` folds it into run.toml when staging has no git.
const StampFile = ".mu-origin.toml"

// FindRoot walks up from path to the enclosing git repo root — the project root
// per the structure contract (no project.toml marker until the sim manager needs
// one).
func FindRoot(path string) (string, error) {
	dir, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s is not inside a git project", path)
		}
		dir = parent
	}
}

// HomeRel names path in the shared relative namespace: its path relative to
// $HOME — the rel that addresses the same thing on every system's tiers.
func HomeRel(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(home, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside $HOME — the mirror namespace is $HOME-relative", path)
	}
	return rel, nil
}

// Stamp is the submit-origin provenance: the fields prep can't reconstruct on
// the cluster. Dirty is case-scoped (uncommitted edits under the case dir), the
// same rule prep applies when it has a checkout.
type Stamp struct {
	Case   string `toml:"case,omitempty"`
	Commit string `toml:"commit,omitempty"`
	Dirty  bool   `toml:"dirty"`
}

// NewStamp reads the case's provenance from the submitting machine's checkout.
// No commits yet (or no git) → commit absent and dirty true: without a sha
// nothing is reproducible, so the record must not look clean.
func NewStamp(caseDir string) Stamp {
	s := Stamp{Dirty: true}
	if rel, ok := gitOut(caseDir, "rev-parse", "--show-prefix"); ok {
		s.Case = strings.TrimSuffix(rel, "/")
	}
	if commit, ok := gitOut(caseDir, "rev-parse", "HEAD"); ok {
		s.Commit = commit
		status, _ := gitOut(caseDir, "status", "--porcelain", "--", ".")
		s.Dirty = status != ""
	}
	return s
}

// TOML renders the stamp as the StampFile payload.
func (s Stamp) TOML() string {
	b, err := toml.Marshal(s)
	if err != nil {
		return ""
	}
	return "# submit-origin provenance — written by `mu project submit` on the submitting machine\n" + string(b)
}

// ReadStamp loads dir's StampFile; ok=false when absent or unparseable —
// provenance degrades to fewer run.toml fields, never to an error.
func ReadStamp(dir string) (Stamp, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, StampFile))
	if err != nil {
		return Stamp{}, false
	}
	var s Stamp
	if err := toml.Unmarshal(raw, &s); err != nil {
		return Stamp{}, false
	}
	return s, true
}

// gitOut runs git in dir and returns trimmed stdout; ok=false outside a repo or
// without git.
func gitOut(dir string, args ...string) (string, bool) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
