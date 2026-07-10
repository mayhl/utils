package sshfs

import (
	"strings"
	"time"
)

// Active is one live entry from the system mount table.
type Active struct {
	Device string
	Dir    string
	Type   string
}

// ActiveMounts parses the system mount table (one bounded `mount` exec — never
// touches the filesystems themselves, so hung mounts can't block the scan).
func ActiveMounts() []Active {
	out, ok := runOut(5*time.Second, "mount")
	if !ok {
		return nil
	}
	return parseMounts(out)
}

// parseMounts handles both mount-table dialects: Linux
// `dev on /dir type fuse.sshfs (opts)` and macOS `dev on /dir (nfs, opts)`.
// The macOS split is on the LAST " (" so a dir containing " (" still parses.
func parseMounts(out string) []Active {
	var res []Active
	for _, ln := range strings.Split(out, "\n") {
		i := strings.Index(ln, " on ")
		if i < 0 {
			continue
		}
		dev, rest := ln[:i], ln[i+4:]
		var dir, typ string
		if j := strings.Index(rest, " type "); j >= 0 {
			dir, typ = rest[:j], rest[j+6:]
			if k := strings.IndexByte(typ, ' '); k >= 0 {
				typ = typ[:k]
			}
		} else if j := strings.LastIndex(rest, " ("); j >= 0 {
			dir, typ = rest[:j], strings.TrimSuffix(rest[j+2:], ")")
			if k := strings.IndexByte(typ, ','); k >= 0 {
				typ = typ[:k]
			}
		} else {
			continue
		}
		res = append(res, Active{Device: dev, Dir: dir, Type: strings.TrimSpace(typ)})
	}
	return res
}

// FuseLike reports whether an entry belongs to the sshfs plane's world: any FUSE
// type (macfuse/osxfuse/fuse.sshfs), or a loopback NFS mount — fuse-t implements
// sshfs as a userspace NFS server on 127.0.0.1, so that's what macOS shows.
func (a Active) FuseLike() bool {
	t := strings.ToLower(a.Type)
	if strings.Contains(t, "fuse") {
		return true
	}
	if strings.HasPrefix(t, "nfs") {
		return strings.HasPrefix(a.Device, "127.0.0.1:") || strings.HasPrefix(a.Device, "localhost:")
	}
	return false
}

// Responds reports whether a mounted dir answers a bounded listing — false means
// the mount is hung (endpoint gone) even though the mount table still shows it.
func Responds(mdir string) bool { return responds(mdir) }
