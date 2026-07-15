package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/project"
	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/rsync"
	"github.com/mayhl/mayhl_utils/internal/shell"
)

// syncTiers are the SHARED-zone tiers `mu project sync` pushes by default: the
// sim-ready model-format data runs depend on, cross-cluster consistent. Pushed to
// $WORKDIR at the same $HOME-relative path. data/processed (transient staging) and
// data/raw (→ $HOME, opt-in) join via the tier selector in a later phase.
var syncTiers = []string{"simulations/data"}

// mtimeWindowSec is the mtime skew tolerance for the differs check: files matching
// on size and within this many seconds count as identical, absorbing FS-granularity
// and timezone-second noise between the laptop and the cluster. The coarse hours/days
// "dest ~3d older" hint is a later phase; here the check is just cheap size + mtime,
// never a full checksum (impractical on 10-50 GB scientific outputs).
const mtimeWindowSec = 2

// syncResult is one tier's classification: what a push would create (new) vs the
// existing files that differ (update — skipped by the additive default).
type syncResult struct {
	rel      string
	localAbs string
	newN     int
	updates  []string
}

// projectSyncCmd is `mu project sync <node>`: the production-run data path. It pushes
// the project's SHARED-zone run-dependency data (simulations/data) to a cluster's
// $WORKDIR at the same $HOME-relative path, additively — new files transfer, existing
// files are never overwritten (add-only house rule). Differing files are reported and
// skipped, not resolved. Distinct from submit-iterate's disposable case staging.
func projectSyncCmd() *cobra.Command {
	var yes, dryRun bool
	c := &cobra.Command{
		Use:   "sync <node>",
		Short: "Push shared run-dependency data (simulations/data) to a cluster's $WORKDIR.",
		Long: "Push the project's shared-zone data — simulations/data, the sim-ready\n" +
			"model-format inputs runs depend on — to the target's $WORKDIR at the same\n" +
			"$HOME-relative path. The production-run data path, distinct from submit's\n" +
			"disposable case staging.\n\n" +
			"Additive and add-only: a dry itemized pass classifies each file as new (dest\n" +
			"absent), identical, or differing (size or mtime); the real transfer pushes only\n" +
			"the new ones (rsync --ignore-existing). Differing files are listed and SKIPPED —\n" +
			"shared data is never silently overwritten. Detection is cheap size + mtime, not\n" +
			"a full checksum. Shows the plan and confirms before transferring.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return projectSync(args[0], yes, dryRun, render.IsVerbose())
		},
	}
	setHelpArgs(c, [2]string{"<node>", "cluster to push shared data to"})
	f := c.Flags()
	f.BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	f.BoolVarP(&dryRun, "dry-run", "n", false, "classify and show the plan without transferring")
	c.ValidArgsFunction = func(_ *cobra.Command, args []string, tc string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	}
	return c
}

// projectSync resolves the project (from cwd), classifies every shared tier that
// exists locally, presents one combined plan, and — unless a dry run, and after a
// single confirm — pushes the new files additively. One pass classifies, a separate
// additive pass transfers, so the report and the write never disagree.
func projectSync(node string, yes, dryRun, verbose bool) error {
	root, err := project.FindRoot(".")
	if err != nil {
		return usageErr("%s", err)
	}
	target, err := hpc.Resolve(node)
	if err != nil {
		return usageErr("%s", err)
	}

	// Which shared tiers actually exist locally (a project may hold only some).
	type tier struct{ rel, localAbs string }
	var tiers []tier
	for _, t := range syncTiers {
		abs := filepath.Join(root, t)
		if fi, serr := os.Stat(abs); serr != nil || !fi.IsDir() {
			continue
		}
		rel, rerr := project.HomeRel(abs)
		if rerr != nil {
			return usageErr("%s", rerr)
		}
		tiers = append(tiers, tier{rel: rel, localAbs: abs})
	}
	if len(tiers) == 0 {
		render.Info("no shared-zone data to sync under " + root + " (looked for: " + strings.Join(syncTiers, ", ") + ")")
		return nil
	}

	render.Info("Sync shared data → " + node)
	render.Detail("project: " + root)

	if err := hpc.EnsureTicket(); err != nil {
		return runErr("%s", err)
	}

	// Classify pass: resolve each tier's $WORKDIR dest (no mkdir — a dry compare
	// against a missing dir correctly reports every file as new) and itemize.
	results := make([]syncResult, 0, len(tiers))
	totalNew, totalUpd := 0, 0
	for _, t := range tiers {
		dest, derr := resolveRemoteDir(target, node, t.rel)
		if derr != nil {
			return derr
		}
		newN, updates, cerr := classifySync(target, t.localAbs, dest)
		if cerr != nil {
			return runErr("%s: classify %s: %s", node, t.rel, cerr)
		}
		results = append(results, syncResult{rel: t.rel, localAbs: t.localAbs, newN: newN, updates: updates})
		totalNew += newN
		totalUpd += len(updates)
		render.Detail(fmt.Sprintf("tier:    %s → $WORKDIR/%s  (%d new, %d differ)", t.rel, t.rel, newN, len(updates)))
	}

	if totalUpd > 0 {
		render.Warn(fmt.Sprintf("%d file(s) differ on %s and will be SKIPPED (add-only — new version = new filename)", totalUpd, node))
		listUpdates(results)
	}
	if totalNew == 0 {
		if totalUpd == 0 {
			render.OK(node + ": already in sync")
		} else {
			render.Info("nothing new to push (differing files skipped)")
		}
		return nil
	}
	render.Detail(fmt.Sprintf("plan:    push %d new file(s), skip %d differing", totalNew, totalUpd))

	if dryRun {
		render.Info("dry run — nothing transferred")
		return nil
	}
	if !yes {
		fmt.Fprintf(os.Stderr, "push %d new file(s) to %s? [y/N] ", totalNew, node)
		var r string
		_, _ = fmt.Scanln(&r)
		if strings.ToLower(strings.TrimSpace(r)) != "y" {
			render.Info("aborted")
			return nil
		}
	}

	// Transfer pass: additive push (rsync --ignore-existing) per tier with new files.
	for _, res := range results {
		if res.newN == 0 {
			continue
		}
		dest, derr := ensureRemoteDir(target, node, res.rel)
		if derr != nil {
			return derr
		}
		o := rsync.Opts{PartialDir: true, Ropt: []string{"--ignore-existing"}}
		label := "sync " + node + " " + res.rel
		code, _ := rsync.Run(rsync.BuildArgs(res.localAbs+"/", target+":"+dest+"/", o), label, verbose)
		if code != 0 {
			render.EventErr("project", fmt.Sprintf("%s FAILED (rsync exit %d)", label, code))
			return codeErr(code)
		}
	}
	msg := fmt.Sprintf("synced %d file(s) → %s", totalNew, node)
	render.OK(msg)
	render.EventOK("project", msg)
	return nil
}

// resolveRemoteDir returns the absolute path of $WORKDIR/<rel> on target without
// creating it — the classify pass needs the path, not the directory (rsync's dry
// compare against a missing dest reports every file as new, which is correct).
// Resolving to an absolute path here keeps $WORKDIR out of rsync's remote arg, where
// shell-expansion is fragile.
func resolveRemoteDir(target, node, rel string) (string, error) {
	qrel := shell.Quote(rel)
	out, err := hpc.RemoteExec(target, fmt.Sprintf(`printf '%%s' "$WORKDIR"/%s`, qrel))
	if err != nil {
		return "", runErr("%s: resolve $WORKDIR: %s", node, err)
	}
	dest := strings.TrimSpace(out)
	if dest == "" || dest == "/"+rel {
		return "", runErr("%s: $WORKDIR is unset on the target", node)
	}
	return dest, nil
}

// ensureRemoteDir resolves $WORKDIR/<rel> and mkdir -p's it, returning the absolute
// path — rsync creates only the final dest dir, not a missing chain, so the parent
// must exist before the transfer.
func ensureRemoteDir(target, node, rel string) (string, error) {
	qrel := shell.Quote(rel)
	out, err := hpc.RemoteExec(target, fmt.Sprintf(`mkdir -p "$WORKDIR"/%s && cd "$WORKDIR"/%s && pwd`, qrel, qrel))
	if err != nil {
		return "", runErr("%s: staging dir: %s", node, err)
	}
	dest := strings.TrimSpace(out)
	if dest == "" {
		return "", runErr("%s: staging dir: empty $WORKDIR resolution", node)
	}
	return dest, nil
}

// classifySync runs a dry itemized compare (size + mtime quick-check, never -c) of
// srcAbs against the remote destAbs and splits the would-transfer files into new
// (dest absent) and update (dest exists, differs). Identical files never itemize.
// It builds the rsync args directly rather than via BuildArgs so the env layer's -u
// (skip files newer on the receiver) can't hide a remote-newer file from the differs
// report; the real transfer, which does inherit the env layer, only ever pushes new
// files anyway.
func classifySync(target, srcAbs, destAbs string) (newN int, updates []string, err error) {
	transport := strings.TrimSpace(config.SSHCommand() + " " + config.SSHTransferOpts())
	args := []string{
		"-a", "-i", "-n", fmt.Sprintf("--modify-window=%d", mtimeWindowSec),
		"-e", transport, srcAbs + "/", target + ":" + destAbs + "/",
	}
	cmd := exec.Command("rsync", args...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, rerr := cmd.Output()
	if rerr != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = rerr.Error()
		}
		return 0, nil, fmt.Errorf("%s", msg)
	}
	newN, updates = classifyItemize(out)
	return newN, updates, nil
}

// classifyItemize splits the raw output of `rsync --itemize-changes -n` into new and
// differing regular files. A transferred file itemizes with leading '<' (sent — the
// push case) or '>' (received — a pull); all-'+' attribute flags mean newly created
// (new), anything else means it exists and differs (update). Non-file lines and
// attr-only ('.') / dir-create ('c') lines are ignored.
func classifyItemize(out []byte) (newN int, updates []string) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // long paths
	for sc.Scan() {
		item, path, ok := parseItemize(sc.Text())
		if !ok || item[1] != 'f' || (item[0] != '<' && item[0] != '>') { // regular files being transferred
			continue
		}
		if item[2:] == "+++++++++" {
			newN++
		} else {
			updates = append(updates, path)
		}
	}
	return newN, updates
}

// parseItemize splits an `rsync --itemize-changes` line into its 11-char flag field
// and the path. ok=false for anything that isn't an itemized change (blank lines,
// stats, messages). The flag field is "YXcstpoguax": Y=update type, X=file type, the
// rest per-attribute (+ = newly created).
func parseItemize(line string) (item, path string, ok bool) {
	if len(line) < 13 || line[11] != ' ' {
		return "", "", false
	}
	item, path = line[:11], line[12:]
	switch item[1] { // file type must be a known code
	case 'f', 'd', 'L', 'D', 'S':
		return item, path, true
	}
	return "", "", false
}

// listUpdates prints the differing files (capped) so the skip is auditable, not
// silent — the add-only rule is "refuse + list, never resolve".
func listUpdates(results []syncResult) {
	const cap_ = 20
	shown := 0
	for _, res := range results {
		for _, u := range res.updates {
			if shown >= cap_ {
				render.Detail(fmt.Sprintf("  ... and %d more", countUpdates(results)-cap_))
				return
			}
			render.Detail("  differs: " + u)
			shown++
		}
	}
}

func countUpdates(results []syncResult) int {
	n := 0
	for _, res := range results {
		n += len(res.updates)
	}
	return n
}
