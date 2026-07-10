package sshfs

import (
	"reflect"
	"testing"
)

// TestParseMounts covers both mount-table dialects and the FuseLike gate: fuse-t
// (loopback NFS) and macfuse on macOS, fuse.sshfs on Linux; remote NFS and apfs
// must not read as sshfs-plane mounts.
func TestParseMounts(t *testing.T) {
	out := `/dev/disk3s1 on / (apfs, sealed, local, read-only, journaled)
127.0.0.1:/Users/alice/hpc_sshfs/mounts/proj on /Users/alice/hpc_sshfs/mounts/proj (nfs, nodev, nosuid, mounted by alice)
alice@hpc1:/p/work on /Users/alice/other (macfuse, nodev, synchronous)
nfshost:/export on /mnt/backup (nfs)
alice@hpc2:/p/work on /home/alice/hpc_sshfs/mounts/wk type fuse.sshfs (rw,nosuid,nodev)
garbage line without the separator`
	got := parseMounts(out)
	want := []Active{
		{"/dev/disk3s1", "/", "apfs"},
		{"127.0.0.1:/Users/alice/hpc_sshfs/mounts/proj", "/Users/alice/hpc_sshfs/mounts/proj", "nfs"},
		{"alice@hpc1:/p/work", "/Users/alice/other", "macfuse"},
		{"nfshost:/export", "/mnt/backup", "nfs"},
		{"alice@hpc2:/p/work", "/home/alice/hpc_sshfs/mounts/wk", "fuse.sshfs"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseMounts:\n got %v\nwant %v", got, want)
	}

	fuseLike := []bool{false, true, true, false, true}
	for i, a := range got {
		if a.FuseLike() != fuseLike[i] {
			t.Errorf("FuseLike(%v) = %v, want %v", a, a.FuseLike(), fuseLike[i])
		}
	}
}
