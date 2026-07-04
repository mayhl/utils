package sshfs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestRegistryRoundTrip writes a registry (incl. a read-only entry and a path
// with spaces) and reads it back, verifying tab-separated parsing and the ro flag.
func TestRegistryRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MU_SSHFS_ROOT", root)

	in := map[string]Mount{
		"funwave":       {Node: "node1", Path: "/p/work/funwave", RO: false},
		"scratch": {Node: "node2", Path: "/archive/nav drift", RO: true},
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
	if MountDir("funwave") != filepath.Join(root, "mounts", "funwave") {
		t.Errorf("MountDir = %s", MountDir("funwave"))
	}
}

// TestReadRegistrySkipsCommentsAndBlanks ensures header comments and blank lines
// are ignored, and a bare (non-ro) 3-field line parses as read-write.
func TestReadRegistrySkipsCommentsAndBlanks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MU_SSHFS_ROOT", root)
	body := "# a comment\n\nfoo\tmike\t/home/foo\nbar\tgold\t/scratch/bar\tro\n"
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
