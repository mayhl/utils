// Package cli assembles the Cobra command tree for `mu`. Charm's fang wraps the
// root at Execute time to style help/errors in the house visual language.
package cli

import (
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/render"
)

// HelpColorScheme themes fang's help/usage in the house language: ANSI colors
// (theme-aware, matching render's tables) instead of fang's truecolor default, with
// the title in cyan (ANSI 6) to match render.StatusTable titles. Chrome only —
// meaning still rides on glyphs, not color.
func HelpColorScheme(c lipgloss.LightDarkFunc) fang.ColorScheme {
	s := fang.AnsiColorScheme(c)
	s.Title = lipgloss.Color("6") // cyan — matches table titles
	return s
}

// Root builds the top-level `mu` command with all subcommand trees attached.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "mu",
		Short: "mayhl_utils — HPC toolkit",
		// fang/main own error + usage printing; RunE returns bare errors for
		// exit codes without Cobra also dumping usage on a runtime failure.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&render.PlainFlag, "plain", false,
		"borderless, tab-aligned tables (auto when piped; overrides MU_RENDER)")
	root.AddCommand(cpCmd(), sshfsCmd(), tarCmd(), hpcCmd(), shellInitCmd(), logCmd(), doctorCmd(), psCmd())
	return root
}
