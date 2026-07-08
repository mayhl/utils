package cli

import (
	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/git"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// gitCmd is the opt-in `mu git` module (gated by MU_MODULES=git): pretty, read-only
// views of the .config signwip/pushsigned workflow. It never signs or pushes — the
// shell `gsw`/`gps` remain the source of truth (bootstrap-safe).
func gitCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "git",
		Short: "Pretty, read-only views of the git signwip/pushsigned workflow.",
		Long: "Colored previews of the .config git workflow — what `gsw`/`gps` would sign\n" +
			"or push, in the house visual language. Read-only: the shell tools still do the\n" +
			"actual signing/pushing. Opt-in via MU_MODULES=git.",
	}
	sw, ps, dr := gitSignwipCmd(), gitPushsignedCmd(), gitDoctorCmd()
	setHelpLabel(sw, "preview", render.HueLoc)
	setHelpLabel(ps, "preview", render.HueLoc)
	setHelpLabel(dr, "check", render.HueUser)
	c.AddCommand(sw, ps, dr)
	// The house help renderer is inherited from root (wrapHelp(root)); here we just
	// give the module its heading, MU_MODULES-gated badge, and shell-front-door panel.
	setHelpTitle(c, "Git Workflow Previews")
	setHelpLabel(c, "opt-in", render.HueGroup)
	setHelpShortcuts(
		c,
		[2]string{"gsw", "git signwip — sign the reviewed WIP (previews via mu git)"},
		[2]string{"gps", "git pushsigned — push the signed prefix (previews via mu git)"},
	)
	return c
}

func gitSignwipCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "signwip",
		Short: "Which unsigned WIP would sign vs skip ([unreviewed]).",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := git.SignwipPreview()
			if err != nil {
				return err
			}
			if !s.HasBase {
				render.Warn("no signed commit to base on")
				return nil
			}
			render.GitSignwip(s)
			return nil
		},
	}
}

func gitPushsignedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pushsigned",
		Short: "The contiguous signed prefix that would push vs held WIP.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := git.PushsignedPreview()
			if err != nil {
				return err
			}
			if !p.HasUpstream {
				render.Warn("no upstream set for the current branch")
				return nil
			}
			render.GitPushsigned(p)
			return nil
		},
	}
}

func gitDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "git on PATH and the .config git workflow files present.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			d := git.DoctorReport()
			rows := make([]render.StatusRow, 0, len(d.Files)+1)
			if d.GitPath != "" {
				rows = append(rows, render.StatusRow{Level: "ok", Name: "git", Detail: d.GitPath})
			} else {
				rows = append(rows, render.StatusRow{Level: "error", Name: "git", Detail: "not found on PATH"})
			}
			for _, f := range d.Files {
				if f.Exists {
					rows = append(rows, render.StatusRow{Level: "ok", Name: f.Name, Detail: f.Path})
				} else {
					rows = append(rows, render.StatusRow{Level: "warn", Name: f.Name, Detail: f.Path + " (missing)"})
				}
			}
			render.StatusTable("git — doctor", rows)
			return nil
		},
	}
}
