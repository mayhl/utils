package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinkChecks(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "config", "checks.d")
	target := filepath.Join(tmp, "share", "mayhl_utils", "checks.d")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	// Fresh link: parents created, symlink lands on src.
	if err := linkChecks(src, target); err != nil {
		t.Fatalf("fresh link: %v", err)
	}
	if got, _ := os.Readlink(target); resolveLink(target, got) != src {
		t.Fatalf("link points at %q, want %q", got, src)
	}

	// Idempotent: second run leaves it alone.
	if err := linkChecks(src, target); err != nil {
		t.Fatalf("re-run: %v", err)
	}

	// Stale link elsewhere: repointed.
	other := filepath.Join(tmp, "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, target); err != nil {
		t.Fatal(err)
	}
	if err := linkChecks(src, target); err != nil {
		t.Fatalf("repoint: %v", err)
	}
	if got, _ := os.Readlink(target); resolveLink(target, got) != src {
		t.Fatalf("after repoint link points at %q, want %q", got, src)
	}

	// Real directory at target: refused, left untouched.
	realDir := filepath.Join(tmp, "realdir", "checks.d")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkChecks(src, realDir); err == nil {
		t.Fatal("real dir at target: want refusal, got nil")
	}
	if fi, err := os.Lstat(realDir); err != nil || !fi.IsDir() {
		t.Fatal("real dir was disturbed")
	}

	// Missing source: refused.
	if err := linkChecks(filepath.Join(tmp, "nope"), filepath.Join(tmp, "t2")); err == nil {
		t.Fatal("missing source: want error, got nil")
	}
}
