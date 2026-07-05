package cli

import (
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
		Short: "List configured mounts with live status. (shortcut: hls)",
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
	c := &cobra.Command{
		Use:               "mount <name>",
		Short:             "Mount a configured name. Auto-remounts a stale mount. (shortcut: hcd — mounts + cds)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: mountCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			os.Exit(runMount(args[0], verbose))
			return nil
		},
	}
	c.Flags().BoolVarP(&verbose, "verbose", "v", false, "show the remote target + verbose ssh output")
	return c
}

// Mount deadlines: a healthy sshfs daemonizes within a second or two, so these only
// bite a genuine hang. Verbose gets a longer window to answer a host-key/Kerberos
// prompt before the deadline.
const (
	mountTimeout        = 30 * time.Second
	mountTimeoutVerbose = 2 * time.Minute
)

// runBounded runs cmd, killing it if it doesn't finish within timeout (so a wedged
// sshfs — dead server, missing remote path — can't hang the terminal). Returns the
// exit code and whether it was killed on timeout. No Setpgid: cmd stays in the
// terminal's foreground group so prompts remain answerable.
func runBounded(cmd *exec.Cmd, timeout time.Duration) (int, bool) {
	if err := cmd.Start(); err != nil {
		render.Err(err.Error())
		return 1, false
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return procExit(err), false
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return 1, true
	}
}

// runMount ports sshfs.py's mount: idempotent, remounts a stale/hung mount, keeps
// stdin/stderr on the terminal so host-key/Kerberos prompts are answerable, and
// shows a spinner while sshfs settles (non-verbose only).
func runMount(name string, verbose bool) int {
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
	render.Info(fmt.Sprintf("connecting %s → %s%s", name, m.Node, roTag))
	if verbose {
		render.Detail("  local   " + mdir)
		render.Detail("  remote  " + m.Path)
	}

	cmd := exec.Command("sshfs", sshfs.MountArgs(target, m.Path, mdir, m.RO, verbose)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Bound the mount so a wedged sshfs can't hang the terminal (the old failure: a
	// bad remote path left it spinning until ^Z). Verbose gets a longer deadline for
	// an interactive prompt.
	timeout := mountTimeout
	var rc int
	var timedOut bool
	if verbose {
		timeout = mountTimeoutVerbose
		rc, timedOut = runBounded(cmd, timeout)
	} else {
		sp := render.NewSpinner("mounting " + name + "…")
		sp.Start()
		rc, timedOut = runBounded(cmd, timeout)
		sp.Stop()
	}

	if timedOut {
		render.Err(fmt.Sprintf("%s: mount timed out after %s — remote path may be missing or the server unreachable; check `mu sshfs list` and retry with -v", name, timeout))
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
	return &cobra.Command{
		Use:               "umount <name>",
		Short:             "Unmount a mount. (shortcut: hum)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: mountCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			mdir := sshfs.MountDir(name)
			if !sshfs.IsMounted(mdir) {
				render.Info(name + ": not mounted")
				return nil
			}
			if sshfs.Umount(mdir) {
				render.OK("unmounted " + name)
				return nil
			}
			render.Err(fmt.Sprintf("%s: couldn't unmount (hung?); try `diskutil unmount force %s` or a restart", name, mdir))
			os.Exit(1)
			return nil
		},
	}
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
		Short: "Register a new mount (name → node:path). Does not mount. (shortcut: hadd)",
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
		Short: "Change an existing mount's node/path or swap ro↔rw. Remounts if live. (shortcut: hset)",
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
  mu sshfs set scratch --rw                                 # temporarily allow writes
  mu sshfs set scratch --node hpc1                      # move to another node`,
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
				os.Exit(runMount(name, false))
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
