package cli

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/project"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// localFile is one local tier file's cheap identity — the same size + mtime signal the
// push classify uses, read here from a local walk instead of an rsync dry pass.
type localFile struct {
	size  int64
	mtime time.Time
}

// projectSyncStatusCmd is `mu project sync status <node>`: the read side of the sync
// manifests. It fetches each tier's .mu-sync.toml from the cluster and reports what is
// staged there — file count, source commit, last push — plus the drift vs the local tree:
// files changed locally since their push, staged files missing locally, and local files
// never pushed. Read-only: it re-reads no data bytes on either end (the manifest IS the
// remote record; --verify at push time is what re-hashes).
func projectSyncStatusCmd() *cobra.Command {
	var tierSel []string
	c := &cobra.Command{
		Use:   "status <node>",
		Short: "Show what shared data is staged on a cluster, per its sync manifests.",
		Long: "Read each tier's sync manifest (.mu-sync.toml) from the target and report\n" +
			"what is staged there and how it drifts from the local tree: files changed\n" +
			"locally since their push, staged files missing locally, and local files never\n" +
			"pushed.\n\n" +
			"Read-only and cheap — the comparison is the manifest's recorded size + mtime\n" +
			"against a local stat, never a re-read of the data bytes. All tiers are checked\n" +
			"by default (reading is free); --tier narrows.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return projectSyncStatus(projSyncOpts{node: args[0], tierSel: tierSel, verbose: render.IsVerbose()})
		},
	}
	setHelpArgs(c, [2]string{"<node>", "cluster to read sync manifests from"})
	c.Flags().StringSliceVar(&tierSel, "tier", nil, "tiers to check: sim, processed, raw (default: all)")
	c.ValidArgsFunction = func(_ *cobra.Command, args []string, tc string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return c
}

// projectSyncStatus fetches the selected tiers' manifests and renders each one's staged
// summary + local drift. Unlike the push, the default is every tier — a status read costs
// one cat, so there is no reason to default narrow.
func projectSyncStatus(o projSyncOpts) error {
	root, err := project.FindRoot(".")
	if err != nil {
		return usageErr("%s", err)
	}
	if len(o.tierSel) == 0 {
		for _, t := range syncTiers {
			o.tierSel = append(o.tierSel, t.name)
		}
	}
	specs, err := resolveTiers(o.tierSel)
	if err != nil {
		return usageErr("%s", err)
	}

	target, err := hpc.Resolve(o.node)
	if err != nil {
		return usageErr("%s", err)
	}

	render.Info("Sync status ← " + o.node)
	render.Detail("project: " + root)

	if err := hpc.EnsureTicket(); err != nil {
		return runErr("%s", err)
	}

	clean := true
	for _, t := range specs {
		abs := filepath.Join(root, t.rel)
		hrel, herr := project.HomeRel(abs)
		if herr != nil {
			return usageErr("%s", herr)
		}
		dest, derr := resolveRemoteDir(target, o.node, t.remoteRoot, hrel)
		if derr != nil {
			return derr
		}
		m := readManifest(target, dest)
		if len(m.File) == 0 {
			render.Detail(fmt.Sprintf("tier:    %s — no manifest on %s (never synced, or pushed before manifests existed)", t.rel, o.node))
			continue
		}

		local, lerr := listTierFiles(abs)
		if lerr != nil {
			return runErr("scan %s: %s", t.rel, lerr)
		}
		inSync, changed, missing, unpushed := classifyStatus(m, local)
		if len(changed)+len(missing)+len(unpushed) > 0 {
			clean = false
		}

		render.Detail(fmt.Sprintf("tier:    %s — %d staged%s · %d in sync, %d changed locally, %d missing locally · %d unpushed",
			t.rel, len(m.File), lastPush(m), len(inSync), len(changed), len(missing), len(unpushed)))
		listPaths("changed", changed)
		listPaths("missing", missing)
		listPaths("unpushed", unpushed)
		if o.verbose {
			listStaged(m)
		}
	}

	if clean {
		render.OK(o.node + ": staged data matches the manifests")
	} else {
		render.Info("drift shown above — `mu project sync " + o.node + "` pushes the unpushed files (--force to overwrite the changed)")
	}
	return nil
}

// classifyStatus compares a tier's manifest against the local tree, splitting the staged
// entries into in-sync (size matches, mtime within the push classify's window), changed
// (differs locally — a push would flag it), and missing (staged but absent locally). Local
// files with no manifest entry are unpushed. Pure — the remote fetch and local walk stay
// in the caller so this is testable.
func classifyStatus(m syncManifest, local map[string]localFile) (inSync, changed, missing, unpushed []string) {
	staged := make(map[string]bool, len(m.File))
	for _, f := range m.File {
		staged[f.Path] = true
		lf, ok := local[f.Path]
		if !ok {
			missing = append(missing, f.Path)
			continue
		}
		if lf.size != f.Size || !mtimeMatches(f.Mtime, lf.mtime) {
			changed = append(changed, f.Path)
			continue
		}
		inSync = append(inSync, f.Path)
	}
	for p := range local {
		if !staged[p] {
			unpushed = append(unpushed, p)
		}
	}
	sort.Strings(unpushed)
	return inSync, changed, missing, unpushed
}

// mtimeMatches compares a manifest's recorded mtime against a local stat within the same
// tolerance the push classify uses. An unparseable record (hand-edited manifest) matches —
// size is then the sole signal, matching --checksum-off behavior's cheap default.
func mtimeMatches(recorded string, local time.Time) bool {
	t, err := time.Parse(time.RFC3339, recorded)
	if err != nil {
		return true
	}
	d := local.Sub(t)
	if d < 0 {
		d = -d
	}
	return d <= mtimeWindowSec*time.Second
}

// listTierFiles walks a local tier dir into rel → size+mtime, applying the same built-in
// junk excludes as the push so the two sides agree on what counts as data. An absent tier
// dir is an empty map, not an error — every staged file then reports missing locally,
// which is the honest answer. Skips the manifest's own basename in case one was copied
// down by hand.
func listTierFiles(abs string) (map[string]localFile, error) {
	out := map[string]localFile{}
	err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == abs {
				return filepath.SkipAll // tier absent locally — empty, not an error
			}
			return err
		}
		if excludedName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Name() == syncManifestName {
			return nil
		}
		fi, ferr := d.Info()
		if ferr != nil {
			return nil // raced away mid-walk — drop it
		}
		rel, rerr := filepath.Rel(abs, path)
		if rerr != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = localFile{size: fi.Size(), mtime: fi.ModTime()}
		return nil
	})
	return out, err
}

// excludedName reports whether a basename matches the built-in junk set — the status
// walk's mirror of the exclude list rsync gets on push.
func excludedName(name string) bool {
	for _, pat := range builtinExcludes {
		if ok, _ := filepath.Match(pat, name); ok {
			return true
		}
	}
	return false
}

// lastPush renders the manifest's most recent synced stamp as a "· last push Xm ago"
// suffix, empty when no stamp parses.
func lastPush(m syncManifest) string {
	var latest time.Time
	for _, f := range m.File {
		if t, err := time.Parse(time.RFC3339, f.Synced); err == nil && t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		return ""
	}
	// fmtAge's sub-minute form is the phrase "just now" — no " ago" on that one.
	a := fmtAge(time.Since(latest))
	if a == "just now" {
		return " · last push just now"
	}
	return " · last push " + a + " ago"
}

// listStaged prints every manifest entry (verbose only): size, sha presence, source
// commit, and push age — the full staged inventory rather than just the drift.
func listStaged(m syncManifest) {
	for _, f := range m.File {
		sha := "-"
		if f.SHA256 != "" {
			sha = "sha256"
		}
		commit := f.Commit
		if len(commit) > 7 {
			commit = commit[:7]
		}
		if f.Dirty {
			commit += "+dirty"
		}
		age := ""
		if t, err := time.Parse(time.RFC3339, f.Synced); err == nil {
			age = fmtAge(time.Since(t))
		}
		render.Detail(fmt.Sprintf("  %-40s %8s  %-6s  %-14s %s", f.Path, render.HumanBytes(f.Size), sha, commit, age))
	}
}
