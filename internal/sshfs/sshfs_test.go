package sshfs

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// setRoot points the sshfs root at a temp dir via a throwaway config.toml — the
// engine's only root source now that the MU_SSHFS_ROOT env fallback is retired.
// ResetForTest drops the memoized config so the new file is read.
func setRoot(t *testing.T, root string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("[sshfs]\nroot = %q\n", root)
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", cfgPath)
	t.Setenv("MU_ROOT", "")
	config.ResetForTest()
}

// TestRegistryRoundTrip writes a registry (incl. a read-only entry and a path
// with spaces) and reads it back, verifying tab-separated parsing and the ro flag.
func TestRegistryRoundTrip(t *testing.T) {
	root := t.TempDir()
	setRoot(t, root)

	in := map[string]Mount{
		"proj":    {Node: "alpha", Path: "/p/work/proj", RO: false},
		"data_ro": {Node: "beta", Path: "/archive/data set", RO: true},
	}
	if err := WriteRegistry(in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := ReadRegistry()
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, in)
	}

	// Registry file lands at $root/registry, mounts nest under $root/mounts/<name>.
	if RegistryPath() != filepath.Join(root, "registry") {
		t.Errorf("RegistryPath = %s", RegistryPath())
	}
	if MountDir("proj") != filepath.Join(root, "mounts", "proj") {
		t.Errorf("MountDir = %s", MountDir("proj"))
	}
}

// TestReadRegistrySkipsCommentsAndBlanks ensures header comments and blank lines
// are ignored, and a bare (non-ro) 3-field line parses as read-write.
func TestReadRegistrySkipsCommentsAndBlanks(t *testing.T) {
	root := t.TempDir()
	setRoot(t, root)
	body := "# a comment\n\nfoo\talpha\t/home/foo\nbar\tbeta\t/scratch/bar\tro\n"
	if err := os.WriteFile(filepath.Join(root, "registry"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := ReadRegistry()
	if len(reg) != 2 {
		t.Fatalf("want 2 entries, got %d: %v", len(reg), reg)
	}
	if reg["foo"].RO || !reg["bar"].RO {
		t.Errorf("ro flags wrong: %v", reg)
	}
}

func TestMountArgs(t *testing.T) {
	t.Setenv("MU_SSH", "ossh")
	got := MountArgs("me@mike.example", "/data", "/local/mnt", true, false)
	want := []string{
		"-o", "ssh_command=ossh -o ServerAliveInterval=15 -o ServerAliveCountMax=3",
		"-o", "reconnect", "-o", "defer_permissions", "-o", "ro",
		"me@mike.example:/data", "/local/mnt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MountArgs =\n %q\nwant %q", got, want)
	}
	// verbose adds ssh -v; rw omits the ro option.
	v := MountArgs("me@mike.example", "/data", "/local/mnt", false, true)
	if v[1] != "ssh_command=ossh -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -v" {
		t.Errorf("verbose ssh_command = %q", v[1])
	}
	for _, a := range v {
		if a == "ro" {
			t.Errorf("rw mount should not carry -o ro: %q", v)
		}
	}
}
