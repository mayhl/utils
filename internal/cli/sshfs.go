package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/sshfs"
)

func sshfsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sshfs",
		Short: "Mount HPC dirs locally over sshfs (macOS/fuse-t).",
		Long: `Mount HPC dirs locally over sshfs (macOS/fuse-t).

Shortcuts (shell functions — not 1:1 with the subcommands below):
  hcd <name>   mount if needed + cd into it   (mu sshfs mount)
  hmt <name>…  mount, no cd (hmt --all = all)  (mu sshfs mount)
  hls          list mounts with live status   (mu sshfs list)
  hadd         register a new mount           (mu sshfs add)
  hset         change/repoint a mount         (mu sshfs set)
  hum          unmount (hum --all = all live)  (mu sshfs umount)`,
	}
	c.AddCommand(sshfsListCmd(), sshfsMountCmd(), sshfsUmountCmd(), sshfsPathCmd(), sshfsAddCmd(), sshfsSetCmd(), sshfsRmCmd())
	return c
}

// mountCompletion completes the first arg (mount name) from the registry.
func mountCompletion(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return sshfs.CompleteMount(toComplete), cobra.ShellCompDirectiveNoFileComp
}

func sshfsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured mounts with live status.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			reg := sshfs.ReadRegistry()
			if len(reg) == 0 {
				render.Info("no mounts — add one with `mu sshfs add <name> <node> <path>`")
				return nil
			}
			names := make([]string, 0, len(reg))
			for n := range reg {
				names = append(names, n)
			}
			sort.Strings(names)
			rows := make([]render.MountRow, 0, len(reg))
			for _, n := range names {
				m := reg[n]
				rows = append(rows, render.MountRow{
					Name: n, Node: m.Node, Path: m.Path, RO: m.RO, Status: sshfs.Status(n),
				})
			}
			render.MountsTable(rows, sshfs.MountsRoot())
			return nil
		},
	}
}

func sshfsMountCmd() *cobra.Command {
	var verbose bool
	var all bool
	c := &cobra.Command{
		Use:   "mount <name>...",
		Short: "Mount configured names (--all mounts every registered). Auto-remounts a stale mount.",
		Args: func(_ *cobra.Command, args []string) error {
			if all {
				if len(args) != 0 {
					return errors.New("cannot name a mount together with --all")
				}
				return nil
			}
			return cobra.MinimumNArgs(1)(nil, args)
		},
		ValidArgsFunction: mountCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			names := args
			if all {
				names = registeredMountNames()
				if len(names) == 0 {
					render.Info("no mounts — add one with `mu sshfs add <name> <node> <path>`")
					return nil
				}
			}
			if len(names) == 1 && !all {
				os.Exit(runMount(names[0], verbose, "", false)) // classic single-mount path (hcd uses this)
			}
			os.Exit(runMountBatch(names, verbose))
			return nil
		},
	}
	c.Flags().BoolVarP(&verbose, "verbose", "v", false, "show the remote target + verbose ssh output")
	c.Flags().BoolVarP(&all, "all", "a", false, "mount every registered mount")
	return c
}

// registeredMountNames returns all registered mount names, sorted.
func registeredMountNames() []string {
	reg := sshfs.ReadRegistry()
	names := make([]string, 0, len(reg))
	for n := range reg {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// runMountBatch mounts several names in sequence. Non-verbose, it collapses per-mount
// chatter into one "Mounting <name>  N/M" progress line (the settle-spinner, relabelled)
// with each mount's ✓/✗ result accumulating above it, then a final "mounted X/Y" summary.
// Verbose skips the progress line and streams each mount in full (for host-key/Kerberos
// prompts and debugging). Sequential, never parallel: a mount can prompt and draws its own
// spinner, so concurrent would garble both. Returns non-zero if any mount failed.
func runMountBatch(names []string, verbose bool) int {
	total := len(names)
	failed := 0
	for i, name := range names {
		var rc int
		if verbose {
			rc = runMount(name, true, "", false)
		} else {
			rc = runMount(name, false, fmt.Sprintf("Mounting %s  %d/%d", name, i, total), true)
		}
		if rc != 0 {
			failed++
		}
	}
	if failed > 0 {
		render.Warn(fmt.Sprintf("mounted %d/%d (%d failed)", total-failed, total, failed))
		return 1
	}
	render.OK(fmt.Sprintf("mounted %d/%d", total, total))
	return 0
}

// Mount deadlines: a healthy sshfs daemonizes within a second or two, so these only
// bite a genuine hang. Verbose gets a longer window to answer a host-key/Kerberos
// prompt before the deadline.
const (
	mountTimeout        = 30 * time.Second
	mountTimeoutVerbose = 2 * time.Minute
)

// fatalMountErrs are sshfs/ssh stderr lines that mean the mount will never succeed —
// seeing one lets runMount fail immediately instead of waiting out the deadline
// (sshfs prints these but doesn't exit, so the line is the signal, not an exit code).
var fatalMountErrs = []string{
	"No such file or directory",
	"Not a directory",
	"Permission denied",
	"Connection refused",
	"Connection reset",
	"Could not resolve hostname",
	"Host key verification failed",
}

// stderrScanner captures sshfs stderr and signals (once) the first line matching a
// fatal pattern. Used in non-verbose mode so the mount fails fast on a bad path and
// that line — not raw noise on the spinner — is what the user sees.
type stderrScanner struct {
	buf   bytes.Buffer
	fatal chan string
	sent  bool
}

func (w *stderrScanner) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if !w.sent {
		s := w.buf.String()
		for _, pat := range fatalMountErrs {
			if i := strings.Index(s, pat); i >= 0 {
				w.sent = true
				w.fatal <- fatalLine(s, i)
				break
			}
		}
	}
	return len(p), nil
}

// fatalLine returns the trimmed line of s containing byte offset i.
func fatalLine(s string, i int) string {
	start := strings.LastIndexByte(s[:i], '\n') + 1
	end := strings.IndexByte(s[i:], '\n')
	if end < 0 {
		end = len(s)
	} else {
		end += i
	}
	return strings.TrimSpace(s[start:end])
}

// runBounded runs cmd, killing it if it doesn't finish within timeout (so a wedged
// sshfs — dead server, missing remote path — can't hang the terminal). fatal, if
// non-nil, fails the mount as soon as sshfs prints a known-fatal stderr line, so a
// bad path doesn't wait out the whole deadline. Returns the exit code, whether it hit
// the timeout, and the fatal line if seen. No Setpgid: cmd stays in the terminal's
// foreground group so prompts remain answerable.
func runBounded(cmd *exec.Cmd, timeout time.Duration, fatal <-chan string) (int, bool, string) {
	if err := cmd.Start(); err != nil {
		render.Err(err.Error())
		return 1, false, ""
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return procExit(err), false, ""
	case msg := <-fatal:
		_ = cmd.Process.Kill()
		<-done
		return 1, false, msg
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return 1, true, ""
	}
}

// runMount ports sshfs.py's mount: idempotent, remounts a stale/hung mount, keeps
// stdin/stderr on the terminal so host-key/Kerberos prompts are answerable, and
// shows a spinner while sshfs settles (non-verbose only). spinLabel overrides the
// settle-spinner text (batch mode passes a "Mounting <name>  N/M" progress label);
// "" uses the default. quiet drops the pre-mount "connecting" line so a batch shows
// just its progress bar + per-mount result lines.
func runMount(name string, verbose bool, spinLabel string, quiet bool) int {
	if _, err := exec.LookPath("sshfs"); err != nil {
		render.Err("sshfs not found — install fuse-t + sshfs to use mu sshfs")
		return 3
	}
	m, ok := sshfs.ReadRegistry()[name]
	if !ok {
		render.Err(fmt.Sprintf("unknown mount: %s (see `mu sshfs list`)", name))
		return 2
	}
	mdir := sshfs.MountDir(name)

	switch sshfs.Status(name) {
	case "mounted":
		return 0 // already live — idempotent
	case "hung":
		render.Warn(name + ": stale mount — remounting")
		if !sshfs.Umount(mdir) {
			render.Err(fmt.Sprintf("%s: couldn't unmount (hung); try `diskutil unmount force %s` or a restart", name, mdir))
			return 1
		}
	}

	target, err := hpc.Resolve(m.Node)
	if err != nil {
		render.Err(err.Error())
		return 2
	}
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		render.Err(err.Error())
		return 1
	}
	hpc.EnsureTicket()

	roTag := ""
	if m.RO {
		roTag = " (ro)"
	}
	if !quiet {
		render.Info(fmt.Sprintf("connecting %s → %s%s", name, m.Node, roTag))
	}
	if verbose {
		render.Detail("  local   " + mdir)
		render.Detail("  remote  " + m.Path)
	}

	cmd := exec.Command("sshfs", sshfs.MountArgs(target, m.Path, mdir, m.RO, verbose)...)
	cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
	// Bound the mount so a wedged sshfs can't hang the terminal (the old failure: a bad
	// remote path left it spinning until ^Z). Non-verbose captures stderr so it can
	// fail fast on a known-fatal line and show it (instead of raw noise on the spinner);
	// verbose streams stderr to the terminal and gets a longer deadline for a prompt.
	timeout := mountTimeout
	var rc int
	var timedOut bool
	var reason string
	if verbose {
		cmd.Stderr = os.Stderr
		timeout = mountTimeoutVerbose
		rc, timedOut, reason = runBounded(cmd, timeout, nil)
	} else {
		w := &stderrScanner{fatal: make(chan string, 1)}
		cmd.Stderr = w
		label := spinLabel
		if label == "" {
			label = "mounting " + name + "…"
		}
		sp := render.NewSpinner(label)
		sp.Start()
		rc, timedOut, reason = runBounded(cmd, timeout, w.fatal)
		sp.Stop()
	}

	switch {
	case reason != "": // sshfs reported a fatal error — no point waiting out the deadline
		render.Err(fmt.Sprintf("%s: mount failed — %s", name, reason))
		_ = sshfs.Umount(mdir)
		return 1
	case timedOut:
		render.Err(fmt.Sprintf("%s: mount timed out after %s — server slow or unreachable; check `mu sshfs list` and retry with -v", name, timeout))
		_ = sshfs.Umount(mdir) // tear down a half-open mount so it isn't left hung
		return 1
	}
	if rc == 0 && sshfs.IsMounted(mdir) {
		msg := "mounted " + name
		if verbose {
			msg += " → " + m.Path
		}
		render.OK(msg + roTag)
		return 0
	}
	render.Err(fmt.Sprintf("%s: mount failed (sshfs exited %d) — retry with `mu sshfs mount %s -v`", name, rc, name))
	return 1
}

func sshfsUmountCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "umount <name>",
		Short: "Unmount a mount, or every live mount with --all.",
		Args: func(_ *cobra.Command, args []string) error {
			if all {
				if len(args) != 0 {
					return errors.New("cannot name a mount together with --all")
				}
				return nil
			}
			return cobra.ExactArgs(1)(nil, args)
		},
		ValidArgsFunction: mountCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			if all {
				return umountAll()
			}
			if !umountOne(args[0]) {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().BoolVarP(&all, "all", "a", false, "unmount every live mount")
	return c
}

// umountOne unmounts a single mount by name, reporting the outcome. Returns
// false only when a live mount fails to unmount (hung).
func umountOne(name string) bool {
	mdir := sshfs.MountDir(name)
	if !sshfs.IsMounted(mdir) {
		render.Info(name + ": not mounted")
		return true
	}
	if sshfs.Umount(mdir) {
		render.OK("unmounted " + name)
		return true
	}
	render.Err(fmt.Sprintf("%s: couldn't unmount (hung?); try `diskutil unmount force %s` or a restart", name, mdir))
	return false
}

// umountAll unmounts every currently-live registered mount, continuing past a
// hung one and exiting non-zero at the end if any failed.
func umountAll() error {
	reg := sshfs.ReadRegistry()
	names := make([]string, 0, len(reg))
	for n := range reg {
		if sshfs.IsMounted(sshfs.MountDir(n)) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		render.Info("no live mounts")
		return nil
	}
	sort.Strings(names)
	failed := 0
	for _, n := range names {
		if !umountOne(n) {
			failed++
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
	return nil
}

func sshfsPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "path <name>",
		Short:             "Print the local mount dir (used by hcd to cd). stdout = just the path.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: mountCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if _, ok := sshfs.ReadRegistry()[name]; !ok {
				render.Err("unknown mount: " + name)
				os.Exit(2)
			}
			fmt.Println(sshfs.MountDir(name))
			return nil
		},
	}
}

func sshfsAddCmd() *cobra.Command {
	var readOnly bool
	c := &cobra.Command{
		Use:   "add <name> <node> <path>",
		Short: "Register a new mount (name → node:path). Does not mount.",
		Args:  cobra.ExactArgs(3),
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 1 { // completing the node arg
				return hpc.CompleteNode(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			name, node, path := args[0], args[1], args[2]
			reg := sshfs.ReadRegistry()
			if ex, ok := reg[name]; ok {
				render.Warn(fmt.Sprintf("mount '%s' already exists → %s:%s", name, ex.Node, ex.Path))
				os.Exit(1)
			}
			if _, err := hpc.Resolve(node); err != nil { // validate the node resolves
				render.Err(err.Error())
				os.Exit(2)
			}
			reg[name] = sshfs.Mount{Node: node, Path: path, RO: readOnly}
			if err := sshfs.WriteRegistry(reg); err != nil {
				render.Err(err.Error())
				os.Exit(1)
			}
			roTag := ""
			if readOnly {
				roTag = " (ro)"
			}
			render.OK(fmt.Sprintf("added %s → %s:%s%s", name, node, path, roTag))
			return nil
		},
	}
	c.Flags().BoolVar(&readOnly, "ro", false, "mount read-only (data to browse, no writes)")
	c.Flags().BoolVar(&readOnly, "read-only", false, "alias for --ro")
	return c
}

func sshfsSetCmd() *cobra.Command {
	var node, path string
	var ro, rw bool
	c := &cobra.Command{
		Use:   "set <name>",
		Short: "Change an existing mount's node/path or swap ro↔rw. Remounts if live.",
		Long: `Change an existing mount's target or access mode in the registry.

Only the flags you pass change:
  --node   repoint to a different HPC node (validated against your clusters)
  --path   repoint to a different remote path
  --ro/--rw   swap read-only ↔ read-write

sshfs bakes node/path/ro at mount time, so if the mount is live, set umounts and
remounts it to apply. Flipping an /archive (HSM) path to rw warns first — those
writes land on tape.

Examples:
  mu sshfs set scratch --path /archive/project/run   # fix a wrong path
  mu sshfs set scratch --rw                           # temporarily allow writes
  mu sshfs set scratch --node hpc1                    # move to another node`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: mountCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			reg := sshfs.ReadRegistry()
			m, ok := reg[name]
			if !ok {
				render.Err("unknown mount: " + name)
				os.Exit(2)
			}
			if ro && rw {
				render.Err("--ro and --rw are mutually exclusive")
				os.Exit(2)
			}
			changed := false
			if node != "" && node != m.Node {
				if _, err := hpc.Resolve(node); err != nil { // validate the node resolves
					render.Err(err.Error())
					os.Exit(2)
				}
				m.Node, changed = node, true
			}
			if path != "" && path != m.Path {
				m.Path, changed = path, true
			}
			if ro && !m.RO {
				m.RO, changed = true, true
			}
			if rw && m.RO {
				m.RO, changed = false, true
			}
			if !changed {
				render.Info(name + ": no change (pass --node/--path/--ro/--rw)")
				return nil
			}
			if !m.RO && strings.HasPrefix(m.Path, "/archive") {
				render.Warn(name + ": rw on an /archive (HSM) path — writes land on tape; make sure that's intended")
			}
			reg[name] = m
			if err := sshfs.WriteRegistry(reg); err != nil {
				render.Err(err.Error())
				os.Exit(1)
			}
			roTag := ""
			if m.RO {
				roTag = " (ro)"
			}
			render.OK(fmt.Sprintf("set %s → %s:%s%s", name, m.Node, m.Path, roTag))
			// Apply to a live mount: sshfs bakes node/path/ro at mount time, so a
			// change only takes effect on remount.
			if sshfs.IsMounted(sshfs.MountDir(name)) {
				render.Info(name + ": remounting to apply")
				if !sshfs.Umount(sshfs.MountDir(name)) {
					render.Err(name + ": couldn't unmount to remount — unmount manually, then `hcd " + name + "`")
					os.Exit(1)
				}
				os.Exit(runMount(name, false, "", false))
			}
			return nil
		},
	}
	c.Flags().StringVar(&node, "node", "", "repoint to a different node")
	c.Flags().StringVar(&path, "path", "", "repoint to a different remote path")
	c.Flags().BoolVar(&ro, "ro", false, "make read-only (remounts if live)")
	c.Flags().BoolVar(&rw, "rw", false, "make read-write (remounts if live)")
	return c
}

func sshfsRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "rm <name>",
		Short:             "Remove a mount from the registry (does not unmount).",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: mountCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			reg := sshfs.ReadRegistry()
			if _, ok := reg[name]; !ok {
				render.Err("unknown mount: " + name)
				os.Exit(2)
			}
			if sshfs.IsMounted(sshfs.MountDir(name)) {
				render.Warn(name + " still mounted — `mu sshfs umount " + name + "` first")
			}
			delete(reg, name)
			if err := sshfs.WriteRegistry(reg); err != nil {
				render.Err(err.Error())
				os.Exit(1)
			}
			render.OK("removed " + name)
			return nil
		},
	}
}

// procExit extracts a child process's exit code from exec.Cmd.Run's error.
func procExit(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}
