// Package cli assembles the Cobra command tree for `mu`. Charm's fang wraps the
// root at Execute time to style help/errors in the house visual language.
package cli

import (
	"io"
	"os"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/modules"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// onHPC reports whether mu is running on an HPC login/compute node ($BC_HOST set, or the
// MU_SYSTEM override), matching the shell platform seam in init.sh.
func onHPC() bool {
	return os.Getenv("BC_HOST") != "" || os.Getenv("MU_SYSTEM") == "hpc"
}

// HelpColorScheme themes fang's help/usage in the house language: ANSI colors
// (theme-aware, matching render's tables) instead of fang's truecolor default, with
// the title in cyan (ANSI 6) to match render.StatusTable titles. Chrome only —
// meaning still rides on glyphs, not color.
func HelpColorScheme(c lipgloss.LightDarkFunc) fang.ColorScheme {
	s := fang.AnsiColorScheme(c)
	s.Title = lipgloss.Color("6") // cyan — matches table titles
	return s
}

// HouseError replaces fang's black-on-red inverted "ERROR" badge with the house error
// tier line (glyph + message, Plain-aware) — the inverted status pill violates the
// color policy. It writes via render.Err to stderr (the same sink fang passes as w),
// generalizing the render.Err + os.Exit precedent in cp.go across all commands.
func HouseError(_ io.Writer, _ fang.Styles, err error) {
	render.Err(err.Error())
}

// Root builds the top-level `mu` command with all subcommand trees attached.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "mu",
		Short: "mayhl_utils — HPC toolkit",
		Long: "HPC toolkit: SSH/rsync helpers, sshfs mounts, queue and process views,\n" +
			"and the git signwip workflow. Run a command below, or `mu <command> --help`.",
		// fang/main own error + usage printing; RunE returns bare errors for
		// exit codes without Cobra also dumping usage on a runtime failure.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	setHelpTitle(root, "mayhl_utils — HPC toolkit") // for the intercepted house root help
	root.PersistentFlags().BoolVar(&render.PlainFlag, "plain", false,
		"borderless, tab-aligned tables (auto when piped; overrides MU_RENDER)")
	root.AddCommand(cpCmd(), tarCmd(), hpcCmd(), setupCmd(), logCmd(), doctorCmd(), psCmd())
	// sshfs mounts a remote dir onto the LOCAL workstation via fuse — inapplicable on an
	// HPC login node (already on the box, no fuse-t), so register it local-only. Mirrors
	// the shell seam, where the hcd/hmt front-doors live in platform/local.sh.
	if !onHPC() {
		root.AddCommand(sshfsCmd())
	}
	// Opt-in modules (MU_MODULES): core stays always-on; new modules register only
	// when listed, so nothing existing changes for a user who hasn't opted in.
	if modules.Enabled("git") {
		root.AddCommand(gitCmd())
	}
	// shell-init and completion moved under `setup`; keep them reachable at the root
	// as HIDDEN aliases so existing rc lines (`eval "$(mu shell-init)"`, `mu completion
	// zsh`) don't break. Cobra's default completion command stays functional, just
	// hidden from the root menu.
	root.AddCommand(hidden(shellInitCmd()))
	root.CompletionOptions.HiddenDefaultCmd = true
	// House help on every subcommand: each direct child gets the house renderer and its
	// subtree inherits it (Cobra resolves the nearest SetHelpFunc). fang overrides only
	// the ROOT's help func at Execute, so `mu --help` stays fang-styled by necessity;
	// wrapping the children keeps every `mu <cmd> --help` in the house language.
	for _, c := range root.Commands() {
		wrapHelp(c)
	}
	return root
}

// hidden marks a command hidden (kept functional, dropped from help) — for the
// root-level aliases of commands whose home is now a submodule.
func hidden(c *cobra.Command) *cobra.Command {
	c.Hidden = true
	return c
}
