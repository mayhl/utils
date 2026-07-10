package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/doctor"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// checksCmd is `mu setup checks`: link the doctor plugin dir (~/.local/share/
// mayhl_utils/checks.d, MU_CHECKS_DIR override) at your checks source (default
// ~/.config/checks.d). Idempotent — a correct link is a no-op, a stale link is
// repointed; a real directory is never clobbered (moving it aside is your call).
func checksCmd() *cobra.Command {
	var source string
	c := &cobra.Command{
		Use:   "checks",
		Short: "Symlink the doctor checks.d plugin dir to your config.",
		Long: "Link the directory `mu doctor` scans for plugin checks to the one you maintain\n" +
			"(default ~/.config/checks.d). Safe to re-run: an up-to-date link is left alone, a\n" +
			"link elsewhere is repointed, and a real directory is refused rather than replaced.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if source == "" {
				source = filepath.Join(defaultConfigDir(), "checks.d")
			}
			return linkChecks(source, doctor.ChecksDir())
		},
	}
	c.Flags().StringVar(&source, "source", "", "checks.d dir to link to (default ~/.config/checks.d)")
	return c
}

// linkChecks makes target a symlink to source, idempotently. Refuses a target
// that exists as anything but a symlink — that's user data, not ours to replace.
func linkChecks(source, target string) error {
	src, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	if fi, serr := os.Stat(src); serr != nil || !fi.IsDir() {
		return runErr("checks source %s is not a directory", src)
	}
	if fi, lerr := os.Lstat(target); lerr == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return runErr("%s already exists and is not a symlink — move it aside first", target)
		}
		if cur, _ := os.Readlink(target); resolveLink(target, cur) == src {
			render.OK("already linked: " + target + " → " + src)
			return nil
		}
		if rerr := os.Remove(target); rerr != nil {
			return rerr
		}
	}
	if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
		return merr
	}
	if lerr := os.Symlink(src, target); lerr != nil {
		return lerr
	}
	msg := "linked checks.d: " + target + " → " + src
	render.OK(msg)
	render.EventOK("setup", msg)
	return nil
}

// resolveLink absolutizes a (possibly relative) symlink destination against the
// link's own directory, so comparison against the wanted source is path-for-path.
func resolveLink(link, dest string) string {
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(link), dest)
	}
	return filepath.Clean(dest)
}
