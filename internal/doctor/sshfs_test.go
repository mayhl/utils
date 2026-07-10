package doctor

import (
	"strings"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/sshfs"
)

// TestClassifySSHFS drives the pure core through every verdict: unmounted /
// responding / hung registered mounts, an orphan inside the mounts tree, a
// foreign fuse mount elsewhere, and non-fuse mounts staying invisible.
func TestClassifySSHFS(t *testing.T) {
	root := "/u/hpc_sshfs/mounts"
	reg := map[string]sshfs.Mount{
		"down": {Node: "alpha", Path: "/p/work/a"},
		"live": {Node: "beta", Path: "/p/work/b", RO: true},
		"dead": {Node: "gamma", Path: "/p/work/c"},
	}
	active := []sshfs.Active{
		{Device: "127.0.0.1:/x", Dir: root + "/live", Type: "nfs"},
		{Device: "127.0.0.1:/y", Dir: root + "/dead", Type: "nfs"},
		{Device: "127.0.0.1:/z", Dir: root + "/ghost", Type: "nfs"},  // orphan in the tree
		{Device: "u@h:/p", Dir: "/u/elsewhere", Type: "macfuse"},     // foreign fuse
		{Device: "/dev/disk3s1", Dir: "/", Type: "apfs"},             // ignored
		{Device: "nfshost:/export", Dir: "/mnt/backup", Type: "nfs"}, // remote NFS: ignored
	}
	responds := func(dir string) bool { return dir == root+"/live" }

	got := classifySSHFS(reg, active, root, responds)
	byName := map[string]Result{}
	for _, r := range got {
		byName[r.Name] = r
	}

	cases := []struct {
		name   string
		status Status
		detail string
	}{
		{"down", OK, "unmounted · alpha:/p/work/a"},
		{"live", OK, "mounted · beta:/p/work/b · ro"},
		{"dead", Fail, "mu sshfs umount dead"},
		{"ghost", Warn, "not in registry"},
		{"elsewhere", Warn, "outside mu's tree"},
	}
	for _, c := range cases {
		r, seen := byName[c.name]
		if !seen {
			t.Errorf("%s: no result", c.name)
			continue
		}
		if r.Status != c.status || !strings.Contains(r.Detail, c.detail) {
			t.Errorf("%s: got (%v, %q), want (%v, contains %q)", c.name, r.Status, r.Detail, c.status, c.detail)
		}
	}
	if len(got) != len(cases) {
		t.Errorf("result count = %d, want %d (non-fuse mounts must stay invisible): %v", len(got), len(cases), got)
	}

	// Empty world: a single OK placeholder, not an empty table.
	empty := classifySSHFS(nil, nil, root, responds)
	if len(empty) != 1 || empty[0].Status != OK {
		t.Errorf("empty world: got %v, want one OK row", empty)
	}
}
