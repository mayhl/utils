// Package mirror is the path resolver over the mirror-set model: groups of roots
// sharing one relative-path namespace ($HOME / $WORKDIR / $ARCHIVE_HOME by default,
// extra sets from config), with two projections — Swap toggles a set's local pair,
// Archive maps into the archive tree's virtual case-container layout. Pure path
// logic except Swap's existence checks and newest-run pick (navigation needs them).
package mirror

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// Set is one resolved mirror set. Roots order is [permanent, scratch] when 2 —
// arity encodes swap-ability (1 root = archive-only, nowhere to toggle to).
type Set struct {
	Name       string
	Roots      []string
	ArchiveRel string
}

// sets returns the default env set plus the configured extras, roots cleaned and
// ~-expanded. The default pair degrades to permanent-only when $WORKDIR is unset
// (a laptop): archive still resolves, swap errors.
func sets() []Set {
	var out []Set
	var roots []string
	if h, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Clean(h))
	}
	if w := os.Getenv("WORKDIR"); w != "" {
		roots = append(roots, filepath.Clean(w))
	}
	if len(roots) > 0 {
		out = append(out, Set{Name: "default", Roots: roots})
	}
	for _, m := range config.MirrorSets() {
		s := Set{Name: m.Name, ArchiveRel: m.ArchiveRel}
		for _, r := range m.Roots {
			s.Roots = append(s.Roots, filepath.Clean(expandHome(r)))
		}
		if len(s.Roots) > 0 {
			out = append(out, s)
		}
	}
	return out
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// match is a path located inside a set: which root it's under and the rel remainder.
type match struct {
	set     Set
	rootIdx int
	rel     string // "" when the path IS the root
}

// locate finds the set/root containing path — longest root prefix wins across all
// sets, so a group set nested under $HOME beats the default pair.
func locate(path string) (match, bool) {
	best := match{rootIdx: -1}
	bestLen := -1
	for _, s := range sets() {
		for i, r := range s.Roots {
			if path != r && !strings.HasPrefix(path, r+"/") {
				continue
			}
			if len(r) > bestLen {
				bestLen = len(r)
				best = match{set: s, rootIdx: i, rel: strings.TrimPrefix(strings.TrimPrefix(path, r), "/")}
			}
		}
	}
	return best, bestLen >= 0
}

// runSuffix is the run-dir suffix `mu job prep` creates: _<jobid>, with an optional
// -<index> for array subjobs.
var runSuffix = regexp.MustCompile(`_([0-9]+(?:-[0-9]+)?)$`)

// caseRe compiles the configured case glob (a basename pattern: * and ? only)
// into an anchored component regexp.
func caseRe() *regexp.Regexp {
	glob := config.CaseGlob()
	var b strings.Builder
	b.WriteString("^")
	for _, c := range glob {
		switch c {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// classify splits a path component into (case base, run id). A run component
// yields both; a bare case dir yields id ""; a non-case component returns ok=false.
func classify(comp string, re *regexp.Regexp) (base, id string, ok bool) {
	if m := runSuffix.FindStringSubmatch(comp); m != nil && re.MatchString(strings.TrimSuffix(comp, m[0])) {
		return strings.TrimSuffix(comp, m[0]), m[1], true
	}
	if re.MatchString(comp) {
		return comp, "", true
	}
	return "", "", false
}

// ClassifyCase splits a dir basename per the configured case glob: a run dir
// yields (base, jobid), a bare case dir (base, ""), anything else ok=false. The
// exported seam for the archive put wrapper's leaf detection.
func ClassifyCase(comp string) (base, id string, ok bool) {
	return classify(comp, caseRe())
}

// pivot finds the case component in rel (pattern-anchored: any depth, first match).
func pivot(comps []string, re *regexp.Regexp) (idx int, base, id string, ok bool) {
	for i, c := range comps {
		if b, r, k := classify(c, re); k {
			return i, b, r, true
		}
	}
	return -1, "", "", false
}

func splitRel(rel string) []string {
	if rel == "" {
		return nil
	}
	return strings.Split(rel, "/")
}

// Swap maps path to its counterpart on the set's other local root — the
// navigation verb behind the `swap` shell wrapper. Case-aware: a run dir maps to
// the case dir (suffix stripped); a case dir maps to its NEWEST run on the scratch
// side (by mtime — most recently started wins), falling back to the bare staged
// copy. The target must exist: landing a cd on a nonexistent dir hides a mistyped
// path, so that's an error, not a passthrough.
func Swap(path string) (string, error) {
	path, err := absClean(path)
	if err != nil {
		return "", err
	}
	m, found := locate(path)
	if !found {
		return "", fmt.Errorf("%s is not under any mirror set (roots: %s)", path, rootList())
	}
	if len(m.set.Roots) < 2 {
		return "", fmt.Errorf("set %q has no swap tier (single root %s)", m.set.Name, m.set.Roots[0])
	}
	other := m.set.Roots[1-m.rootIdx]
	comps := splitRel(m.rel)
	re := caseRe()

	if i, base, id, ok := pivot(comps, re); ok {
		if id != "" {
			// run → canonical case dir on the other side.
			comps[i] = base
			return checkExists(filepath.Join(append([]string{other}, comps...)...))
		}
		if m.rootIdx == 0 {
			// case dir on permanent → the newest run on scratch; bare staged copy is
			// the fallback so a not-yet-run case still swaps somewhere real.
			parent := filepath.Join(append([]string{other}, comps[:i]...)...)
			target := func(caseComp string) string {
				comps[i] = caseComp
				return filepath.Join(append([]string{other}, comps...)...)
			}
			if run := newestRun(parent, base, re); run != "" {
				if t, err := checkExists(target(run)); err == nil {
					return t, nil
				}
			}
			if t, err := checkExists(target(base)); err == nil {
				return t, nil
			}
			return "", fmt.Errorf("no runs (or staged copy) of %s under %s", base, parent)
		}
	}
	return checkExists(filepath.Join(other, m.rel))
}

// newestRun picks the most recently modified <base>_<id> dir in parent ("" if none).
func newestRun(parent, base string, re *regexp.Regexp) string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	type cand struct {
		name string
		mod  int64
	}
	var cands []cand
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if b, id, ok := classify(e.Name(), re); ok && id != "" && b == base {
			if info, err := e.Info(); err == nil {
				cands = append(cands, cand{e.Name(), info.ModTime().UnixNano()})
			}
		}
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod > cands[j].mod })
	return cands[0].name
}

// Archive maps path into the archive projection — a pure rewrite, no filesystem
// checks (the target is tape). Multi-tier sets get the case-container transform
// (case_X → case_X/input, case_X_<id> → case_X/<id>) and the provenance guards:
// each class archives only from its authoritative tier. Single-root sets are all
// "plain" — 1:1 under their archive_rel, no transform, no guards.
func Archive(path string) (string, error) {
	path, err := absClean(path)
	if err != nil {
		return "", err
	}
	ah := os.Getenv("ARCHIVE_HOME")
	if ah == "" {
		return "", fmt.Errorf("$ARCHIVE_HOME is not set (no archive facility here)")
	}
	m, found := locate(path)
	if !found {
		return "", fmt.Errorf("%s is not under any mirror set (roots: %s)", path, rootList())
	}
	dst := func(rel string) string {
		return filepath.Join(ah, m.set.ArchiveRel, rel)
	}
	if len(m.set.Roots) < 2 {
		return dst(m.rel), nil
	}

	// Shared data: the as-run copy on scratch is the truth; the permanent-side
	// master library is a superset and must not masquerade as provenance.
	dataDir := config.ProjectDataDir()
	if inProjectTier(m.rel, dataDir) {
		if m.rootIdx == 0 {
			return "", fmt.Errorf("shared data archives from the scratch tier — the as-run copy, not the %s master library", m.set.Roots[0])
		}
		return dst(m.rel), nil
	}

	comps := splitRel(m.rel)
	if i, base, id, ok := pivot(comps, caseRe()); ok {
		if id == "" && m.rootIdx != 0 {
			return "", fmt.Errorf("case inputs archive from the permanent tier (%s) — the authored canonical, not a staged copy", m.set.Roots[0])
		}
		if id != "" && m.rootIdx != 1 {
			return "", fmt.Errorf("runs archive from the scratch tier (%s) — the run itself, not a stray copy", m.set.Roots[1])
		}
		if id == "" {
			comps[i] = base + "/input"
		} else {
			comps[i] = base + "/" + id
		}
		return dst(strings.Join(comps, "/")), nil
	}
	return dst(m.rel), nil
}

// inProjectTier reports whether rel sits inside the named project tier at any
// project depth — projects nest freely under a root (~/projects/a/simulations/data),
// so the tier path is matched as a rel-path segment, not a prefix.
func inProjectTier(rel, tier string) bool {
	return rel == tier || strings.HasSuffix(rel, "/"+tier) ||
		strings.Contains(rel, "/"+tier+"/") || strings.HasPrefix(rel, tier+"/")
}

func absClean(path string) (string, error) {
	a, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(a), nil
}

func checkExists(target string) (string, error) {
	if _, err := os.Stat(target); err != nil {
		return "", fmt.Errorf("counterpart %s does not exist", target)
	}
	return target, nil
}

func rootList() string {
	var rs []string
	for _, s := range sets() {
		rs = append(rs, s.Roots...)
	}
	if len(rs) == 0 {
		return "none configured"
	}
	return strings.Join(rs, ", ")
}
