// Package sshfs is the engine for the local-only `mu sshfs` mount plane: a file
// registry of named HPC mounts plus timeout-bounded mount-state inspection, so a
// hung/dead mount reports a status instead of freezing the terminal. Ports the
// retired Python sshfs.py; command orchestration (resolve, Kerberos, spinner)
// lives in internal/cli, rendering in internal/render.
package sshfs

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// Mount is one registry entry: where it points and whether it's read-only.
type Mount struct {
	Node string
	Path string
	RO   bool
}

const statTimeout = 4 * time.Second // slower listing → treat the mount as hung

// Root is the mount parent dir (config sshfs.root / $MU_SSHFS_ROOT, default
// ~/hpc_sshfs), with ~ expanded.
func Root() string {
	return expandHome(config.SSHFSRoot())
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// RegistryPath is the tab-separated registry file; MountsRoot is the parent of
// all mount dirs (nested under mounts/ so a mount named "mounts" can't collide
// with the registry file); MountDir is one mount's local dir.
func RegistryPath() string        { return filepath.Join(Root(), "registry") }
func MountsRoot() string          { return filepath.Join(Root(), "mounts") }
func MountDir(name string) string { return filepath.Join(MountsRoot(), name) }

// ReadRegistry parses the registry file into {name: Mount}. Lines are
// `name<TAB>node<TAB>path[<TAB>ro]`; blanks and #-comments are skipped. A missing
// file is an empty registry, not an error.
func ReadRegistry() map[string]Mount {
	out := map[string]Mount{}
	f, err := os.Open(RegistryPath())
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s := sc.Text()
		if t := strings.TrimSpace(s); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		parts := strings.Split(s, "\t")
		if len(parts) >= 3 {
			ro := len(parts) >= 4 && strings.TrimSpace(parts[3]) == "ro"
			out[strings.TrimSpace(parts[0])] = Mount{
				Node: strings.TrimSpace(parts[1]),
				Path: strings.TrimSpace(parts[2]),
				RO:   ro,
			}
		}
	}
	return out
}

// WriteRegistry rewrites the registry file, entries sorted by name.
func WriteRegistry(entries map[string]Mount) error {
	path := RegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# managed by `mu sshfs add` / `mu sshfs rm` — do not hand-edit lightly\n")
	b.WriteString("# name\tnode\tremote-path\t[ro]\n")
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m := entries[n]
		b.WriteString(n + "\t" + m.Node + "\t" + m.Path)
		if m.RO {
			b.WriteString("\tro")
		}
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// CompleteMount returns registered names starting with prefix (shell completion).
func CompleteMount(prefix string) []string {
	var out []string
	for n := range ReadRegistry() {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// --- mount state (all timeout-bounded — never block on a hung mount) ---------

// runOut runs a command with a timeout, returning its stdout and whether it
// succeeded (exit 0, no timeout).
func runOut(timeout time.Duration, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &buf
	err := cmd.Run()
	return buf.String(), err == nil && ctx.Err() == nil
}

// IsMounted reports whether mdir is an active mountpoint, by parsing `mount`
// output — it never touches the filesystem, so a hung mount can't block it.
func IsMounted(mdir string) bool {
	out, ok := runOut(5*time.Second, "mount")
	if !ok {
		return false
	}
	needle := " on " + mdir + " ("
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, needle) {
			return true
		}
	}
	return false
}

func responds(mdir string) bool {
	_, ok := runOut(statTimeout, "ls", mdir)
	return ok
}

// Status is "mounted" | "hung" | "unmounted", safe even against a hung mount.
func Status(name string) string {
	mdir := MountDir(name)
	if !IsMounted(mdir) {
		return "unmounted"
	}
	if responds(mdir) {
		return "mounted"
	}
	return "hung"
}

// Umount unmounts mdir (plain umount, then diskutil force). Returns false if it
// couldn't. A not-currently-mounted dir is a success.
func Umount(mdir string) bool {
	if !IsMounted(mdir) {
		return true
	}
	for _, c := range [][]string{{"umount", mdir}, {"diskutil", "unmount", "force", mdir}} {
		if _, ok := runOut(10*time.Second, c[0], c[1:]...); ok {
			return true
		}
	}
	return false
}

// MountArgs builds the sshfs argument vector (after the "sshfs" prog): keepalive
// ssh transport via MU_SSH, reconnect, defer_permissions, optional read-only.
func MountArgs(target, rpath, mdir string, ro, verbose bool) []string {
	sshCmd := config.SSHCommand() + " -o ServerAliveInterval=15 -o ServerAliveCountMax=3"
	if verbose {
		sshCmd += " -v"
	}
	args := []string{"-o", "ssh_command=" + sshCmd, "-o", "reconnect", "-o", "defer_permissions"}
	if ro {
		args = append(args, "-o", "ro")
	}
	return append(args, target+":"+rpath, mdir)
}
