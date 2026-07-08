package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// syncCmd is `mu setup sync <node>`: the machine-lifecycle MAINTAIN step. It propagates
// this machine's shared inventory (config.toml — hpc_user, [[cluster]] defs, fleet, prefs)
// to a target, while KEEPING the target's machine-local [ssh]/[sshfs] seams. The sshfs
// mount registry and any secrets live in other files and are never touched. Diff + confirm
// before writing. Reuses onboard's ssh plumbing; skips the birth steps.
func syncCmd() *cobra.Command {
	var muRoot string
	var yes bool
	c := &cobra.Command{
		Use:   "sync <node|user@host>",
		Short: "Push this machine's config.toml (clusters/fleet) to another box.",
		Long: "Propagate this machine's shared inventory — hpc_user, [[cluster]] defs, fleet,\n" +
			"prefs — to a target's config.toml, keeping the target's machine-local [ssh] and\n" +
			"[sshfs] settings. The sshfs mount registry and secrets are never touched. Shows a\n" +
			"diff and confirms before writing.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSync(args[0], muRoot, yes)
		},
	}
	c.Flags().StringVar(&muRoot, "mu-root", "~/.config/mu", "target MU_ROOT (holds config.toml)")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	_ = c.RegisterFlagCompletionFunc("mu-root", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

func runSync(nodeOrTarget, muRoot string, yes bool) error {
	target, err := hpc.Resolve(nodeOrTarget)
	if err != nil {
		render.Err(err.Error())
		os.Exit(2)
	}
	localPath := config.Path()
	if localPath == "" {
		render.Err("no local config.toml to sync from (set MU_CONFIG_FILE or MU_ROOT)")
		os.Exit(1)
	}
	local, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", localPath, err)
	}
	remotePath := muRoot + "/config.toml"
	targetText, _ := captureSSH(target, "cat "+remotePath+" 2>/dev/null") // empty if absent

	// Merge: this machine's inventory minus its own [ssh]/[sshfs], plus the target's
	// seams — so shared inventory propagates and per-machine seams survive.
	rest, _ := splitTOMLSections(string(local), "ssh", "sshfs")
	_, seams := splitTOMLSections(targetText, "ssh", "sshfs")
	merged := assembleConfig(rest, seams)

	if strings.TrimSpace(merged) == strings.TrimSpace(targetText) {
		render.OK(target + ": config.toml already in sync")
		return nil
	}
	showConfigDiff(targetText, merged)
	if !yes {
		fmt.Fprintf(os.Stderr, "write config.toml to %s? [y/N] ", target)
		var r string
		_, _ = fmt.Scanln(&r)
		if strings.ToLower(strings.TrimSpace(r)) != "y" {
			render.Info("aborted")
			return nil
		}
	}
	// Write atomically on the box: stage to .tmp, then mv into place.
	script := "mkdir -p " + muRoot + " && cat > " + remotePath + ".tmp && mv " + remotePath + ".tmp " + remotePath
	if err := pipeSSH(target, script, merged); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	msg := "synced config.toml → " + target + " (kept its [ssh]/[sshfs])"
	render.OK(msg)
	render.EventOK("setup", msg)
	return nil
}

// splitTOMLSections walks TOML text and returns it with the named top-level tables
// removed (rest), plus each removed table's verbatim text (sections). A table runs from
// its header line to the next top-level header or EOF; the root (pre-header) lines are
// always kept. Used to drop the laptop's [ssh]/[sshfs] and splice in the target's.
func splitTOMLSections(text string, names ...string) (rest string, sections map[string]string) {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	sections = map[string]string{}
	var restB strings.Builder
	cur := "" // table currently being captured into sections; "" → goes to rest
	for _, line := range strings.Split(text, "\n") {
		if h, ok := tomlHeader(line); ok {
			if want[h] {
				cur = h
			} else {
				cur = ""
			}
		}
		if cur != "" {
			sections[cur] += line + "\n"
		} else {
			restB.WriteString(line)
			restB.WriteByte('\n')
		}
	}
	return restB.String(), sections
}

// tomlHeader returns the top-level table name of a "[table]" / "[[array]]" / "[a.b]"
// header line (the first path segment), or ok=false for a non-header line.
func tomlHeader(line string) (name string, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "[") {
		return "", false
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "[["), "[")
	end := strings.IndexAny(s, "].")
	if end <= 0 {
		return "", false
	}
	return s[:end], true
}

// assembleConfig joins the inventory body with the preserved seam tables (in a stable
// order), each separated by a blank line.
func assembleConfig(rest string, seams map[string]string) string {
	out := strings.TrimRight(rest, "\n") + "\n"
	for _, name := range []string{"ssh", "sshfs"} {
		if s := strings.TrimRight(seams[name], "\n"); strings.TrimSpace(s) != "" {
			out += "\n" + s + "\n"
		}
	}
	return out
}

// showConfigDiff prints a unified diff of the target's current config.toml vs the merged
// one (— target, + synced) via the system `diff`; on no diff tool it prints the new text.
func showConfigDiff(oldText, newText string) {
	od, err1 := os.CreateTemp("", "cfg-old-*.toml")
	nd, err2 := os.CreateTemp("", "cfg-new-*.toml")
	if err1 != nil || err2 != nil {
		return
	}
	defer func() { _ = os.Remove(od.Name()); _ = os.Remove(nd.Name()) }()
	_, _ = od.WriteString(oldText)
	_, _ = nd.WriteString(newText)
	_ = od.Close()
	_ = nd.Close()
	render.Detail("config.toml changes (— target, + synced):")
	out, _ := exec.Command("diff", "-u", od.Name(), nd.Name()).CombinedOutput() // diff exits 1 when they differ
	fmt.Fprintln(os.Stderr, strings.TrimRight(string(out), "\n"))
}

// captureSSH runs a read command on the target and returns its stdout.
func captureSSH(target, cmd string) (string, error) {
	out, err := exec.Command("ssh", target, cmd).Output()
	return string(out), err
}

// pipeSSH runs a command on the target with stdin fed from content (its stderr surfaced).
func pipeSSH(target, cmd, content string) error {
	c := exec.Command("ssh", target, cmd)
	c.Stdin = strings.NewReader(content)
	c.Stderr = os.Stderr
	return c.Run()
}
