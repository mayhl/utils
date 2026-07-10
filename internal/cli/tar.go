package cli

import (
	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/tar"
)

func tarCmd() *cobra.Command {
	var useGzip bool
	c := &cobra.Command{
		Use:   "tar <path>",
		Short: "Archive a directory or extract an archive.",
		Long: "Archive or extract with a live byte-progress bar, wrapping the system tar.\n\n" +
			"The verb is inferred from <path>: a directory is archived (→ .tar, or .tar.gz\n" +
			"with -z); a .tar/.tar.gz archive is extracted into the current directory\n" +
			"(compression auto-detected). Shell shortcuts: qtar (plain), gtar (gzip),\n" +
			"bqtar/bgtar (backgrounded + reniced).",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// tar.Run renders its own progress/errors; propagate just its exit code
			return codeErr(tar.Run(args[0], useGzip))
		},
	}
	c.Flags().BoolVarP(&useGzip, "gzip", "z", false, "gzip when archiving a dir (→ .tar.gz); ignored on extract")
	return c
}
