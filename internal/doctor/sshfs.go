package doctor

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/sshfs"
)

// SSHFSResults inspects the mount plane: each registered mount's live state, then
// any fuse-like mounts the registry doesn't claim — orphans in mu's tree (a
// registry edit or an external clobber left them behind) and foreign fuse mounts
// elsewhere. Both matter beyond hygiene: the laptop AV traverses every live fuse
// mount, so a forgotten one has a real cost.
func SSHFSResults() []Result {
	return classifySSHFS(sshfs.ReadRegistry(), sshfs.ActiveMounts(), sshfs.MountsRoot(), sshfs.Responds)
}

// classifySSHFS is the pure core (mount table + probe injected for tests).
// Verdicts: unmounted/responding registered mounts are OK, a hung mount FAILs,
// anything fuse-like the registry doesn't know WARNs.
func classifySSHFS(reg map[string]sshfs.Mount, active []sshfs.Active, mountsRoot string, responds func(string) bool) []Result {
	byDir := map[string]sshfs.Active{}
	for _, a := range active {
		byDir[a.Dir] = a
	}

	names := make([]string, 0, len(reg))
	for n := range reg {
		names = append(names, n)
	}
	sort.Strings(names)

	var out []Result
	claimed := map[string]bool{}
	for _, n := range names {
		m := reg[n]
		dir := filepath.Join(mountsRoot, n)
		claimed[dir] = true
		endpoint := m.Node + ":" + m.Path
		if m.RO {
			endpoint += " · ro"
		}
		var r Result
		switch {
		case byDir[dir].Dir == "":
			r = Result{Name: n, Status: OK, Detail: "unmounted · " + endpoint}
		case responds(dir):
			r = Result{Name: n, Status: OK, Detail: "mounted · " + endpoint}
		default:
			r = Result{Name: n, Status: Fail, Detail: "hung — mu sshfs umount " + n}
		}
		r.Section = "sshfs"
		out = append(out, r)
	}

	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		a := byDir[d]
		if !a.FuseLike() || claimed[d] {
			continue
		}
		detail := "fuse mount outside mu's tree (" + a.Device + " on " + d + ")"
		if strings.HasPrefix(d, mountsRoot+string(filepath.Separator)) {
			detail = "mounted but not in registry — mu sshfs add, or umount " + d
		}
		out = append(out, Result{Section: "sshfs", Name: filepath.Base(d), Status: Warn, Detail: detail})
	}

	if len(out) == 0 {
		out = append(out, Result{Section: "sshfs", Name: "sshfs", Status: OK, Detail: "registry empty, no live fuse mounts"})
	}
	return out
}
