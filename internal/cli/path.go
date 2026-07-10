package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/mirror"
)

// pathCmd is `mu path`: the mirror-set resolver — one relative namespace across
// $HOME / $WORKDIR / $ARCHIVE_HOME (plus configured [[mirror_set]] extras), two
// projections. The shell keeps only thin wrappers (`swap` cds on the output);
// all classification lives here.
func pathCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "path",
		Short: "Resolve a path across the mirror tiers (swap / archive).",
		Long: "Rewrite a path between the tiers of its mirror set — the default\n" +
			"$HOME/$WORKDIR/$ARCHIVE_HOME trio, or a configured [[mirror_set]] (longest\n" +
			"root prefix wins, so nested group sets beat the defaults).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	c.AddCommand(pathVerbCmd("swap",
		"Print the path's counterpart on the set's other local tier.",
		"Map a path to the other side of its set's local pair ($HOME ↔ $WORKDIR).\n"+
			"Case-aware navigation: a run dir maps to its case dir (suffix stripped); a case\n"+
			"dir maps to its NEWEST run on scratch (falling back to the bare staged copy).\n"+
			"The target must exist. The shell wrapper is\n\n"+
			"    swap() { local d; d=$(mu path swap \"$@\") && cd \"$d\"; }",
		mirror.Swap))
	c.AddCommand(pathVerbCmd("archive",
		"Print the path's archive projection under $ARCHIVE_HOME.",
		"Map a local path into the archive tree: case dirs land in the virtual container\n"+
			"(case_X → case_X/input, case_X_<jobid> → case_X/<jobid>), shared data 1:1 —\n"+
			"each class only from its authoritative tier (inputs from $HOME, runs and shared\n"+
			"data from $WORKDIR). Pure mapping: nothing is transferred.",
		mirror.Archive))
	return c
}

// pathVerbCmd wraps one projection: `mu path <verb> [path]`, default $PWD.
func pathVerbCmd(verb, short, long string, resolve func(string) (string, error)) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " [path]",
		Short: short,
		Long:  long,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p := ""
			if len(args) == 1 {
				p = args[0]
			} else {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				p = wd
			}
			out, err := resolve(p)
			if err != nil {
				return runErr("%s", err)
			}
			fmt.Println(out)
			return nil
		},
	}
}
