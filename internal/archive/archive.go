// Package archive wraps the site PST/TUSC `archive` command (the HPSS
// front-end) with the mirror projection: the archive-side dir is computed from
// $PWD via mirror.Archive and injected as -C, ARCHIVE_PROBE=yes turns on the
// native size verify, and a flagless `put` packs case material into tar tiers
// before it puts (tape wants few large files). An explicit -C from the caller
// passes through untouched — the wrapper's whole job is inferring it.
package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/mirror"
	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/tar"
)

// Run dispatches one wrapped invocation: sub is the archive subcommand, args
// the rest verbatim. Returns a process exit code (failures already rendered).
func Run(sub string, args []string) int {
	bin, err := exec.LookPath("archive")
	if err != nil {
		render.Err("no `archive` command here — PST/TUSC lives on the HPC side")
		return 1
	}
	if hasArg(args, "-C") {
		return run(bin, "", "", sub, args)
	}
	wd, err := os.Getwd()
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if sub == "put" && len(args) > 0 && !hasFlags(args) {
		return put(bin, wd, args)
	}
	proj, err := mirror.Archive(wd)
	if err != nil {
		render.Err(err.Error())
		return 2
	}
	return run(bin, "", proj, sub, args)
}

// run execs one real archive invocation: `archive <sub> [-C cdir] <args…>` from
// dir ("" = inherit). An injected -C also sets ARCHIVE_PROBE=yes (the native
// before/after size verify); the explicit--C passthrough stays untouched.
func run(bin, dir, cdir, sub string, args []string) int {
	argv := []string{sub}
	if cdir != "" {
		argv = append(argv, "-C", cdir)
	}
	argv = append(argv, args...)
	cmd := exec.Command(bin, argv...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if cdir != "" {
		cmd.Env = append(os.Environ(), "ARCHIVE_PROBE=yes")
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		render.Err("archive: " + err.Error())
		return 1
	}
	return 0
}

// pack is one dir → staged tar → put: the tar stages next to dir and lands at
// dst/name on the archive side.
type pack struct {
	dir  string // local dir to tar
	dst  string // archive-side -C dir
	name string // tar basename, e.g. "250.tar"
}

// put plans and runs the tar tiers for a flagless `archive put`: a case/run
// leaf → one tar at its projection; a parent whose EVERY case leaf is under
// tar_parent_threshold → ONE parent-level tar (the batch is the retrieval
// unit — all-or-nothing, so an oversize run can't hide inside it); otherwise
// tar per leaf. Plain files and non-case dirs pass through to a single native
// put under $PWD's projection.
func put(bin, wd string, args []string) int {
	var packs []pack
	var rest []string
	for _, a := range args {
		abs := a
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(wd, a)
		}
		abs = filepath.Clean(abs)
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			rest = append(rest, a)
			continue
		}
		ps, rc := planDir(abs)
		if rc != 0 {
			return rc
		}
		if ps == nil {
			rest = append(rest, a)
			continue
		}
		packs = append(packs, ps...)
	}
	for _, p := range packs {
		if rc := runPack(bin, p); rc != 0 {
			return rc
		}
	}
	if len(rest) > 0 {
		proj, err := mirror.Archive(wd)
		if err != nil {
			render.Err(err.Error())
			return 2
		}
		return run(bin, "", proj, "put", rest)
	}
	return 0
}

// planDir maps one dir arg to its packs: a case/run leaf packs itself (a guard
// violation — wrong tier — aborts); a parent with case-leaf children packs per
// the batch tier, skipping guarded leaves; anything else returns nil (passthrough).
func planDir(dir string) ([]pack, int) {
	if _, _, ok := mirror.ClassifyCase(filepath.Base(dir)); ok {
		p, err := leafPack(dir)
		if err != nil {
			render.Err(err.Error())
			return nil, 2
		}
		return []pack{p}, 0
	}
	leaves, others := caseLeaves(dir)
	if len(leaves) == 0 {
		return nil, 0
	}
	small := true
	for _, l := range leaves {
		if duBytes(l) >= config.TarParentThreshold() {
			small = false
			break
		}
	}
	if small {
		proj, err := mirror.Archive(dir)
		if err != nil {
			render.Err(err.Error())
			return nil, 2
		}
		return []pack{{dir, filepath.Dir(proj), filepath.Base(proj) + ".tar"}}, 0
	}
	var out []pack
	for _, l := range leaves {
		p, err := leafPack(l)
		if err != nil {
			// a staged bare case beside its runs is normal on scratch — the
			// authored input archives from $HOME, so skip it, don't abort the batch
			render.Warn("skipping " + filepath.Base(l) + " — " + err.Error())
			others++
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		render.Err("nothing packable in " + dir + " — every case leaf skipped")
		return nil, 2
	}
	if others > 0 {
		render.Warn(fmt.Sprintf("%d non-case entries in %s skipped — put them explicitly", others, dir))
	}
	return out, 0
}

// leafPack builds the pack for one case/run leaf: the tar lands AT the leaf's
// projection (…/case_a/250.tar) with the flat local name as the member root, so
// get+extract on scratch recreates the dir exactly. The projection call is also
// the provenance guard (inputs from permanent, runs from scratch).
func leafPack(dir string) (pack, error) {
	proj, err := mirror.Archive(dir)
	if err != nil {
		return pack{}, err
	}
	if sz := duBytes(dir); sz >= config.TarHookThreshold() {
		// FUTURE: hand leaves this size to the model pack hook instead
		render.Warn(fmt.Sprintf("%s is %s — packing one tar; a model pack hook should split it",
			filepath.Base(dir), render.HumanBytes(sz)))
	}
	return pack{dir, filepath.Dir(proj), filepath.Base(proj) + ".tar"}, nil
}

// runPack stages the tar next to the dir, puts it with -D (native
// delete-local-after-verify), and removes any leftover staging either way —
// the belt for a failed put and for stubs that don't honor -D.
func runPack(bin string, p pack) int {
	staging := filepath.Join(filepath.Dir(p.dir), p.name)
	if _, err := os.Stat(staging); err == nil {
		render.Err(staging + " already exists — remove it or put it explicitly")
		return 1
	}
	if rc := tar.CreateRooted(p.dir, staging); rc != 0 {
		return rc
	}
	rc := run(bin, filepath.Dir(p.dir), p.dst, "put", []string{"-D", p.name})
	_ = os.Remove(staging)
	return rc
}

// caseLeaves splits dir's child dirs into case/run leaves and the count of
// everything else (files included — a batch tar takes the whole dir, so the
// count only matters when the parent falls to per-leaf packing).
func caseLeaves(dir string) (leaves []string, others int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}
	for _, e := range entries {
		if _, _, ok := mirror.ClassifyCase(e.Name()); ok && e.IsDir() {
			leaves = append(leaves, filepath.Join(dir, e.Name()))
			continue
		}
		others++
	}
	return leaves, others
}

// duBytes is the best-effort recursive size of dir for the tier thresholds.
func duBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// hasFlags reports any dash-leading arg: value-taking site flags (-retry N)
// make path detection unsafe, so a flagged put skips packing and passes through.
func hasFlags(args []string) bool {
	for _, a := range args {
		if len(a) > 0 && a[0] == '-' {
			return true
		}
	}
	return false
}
