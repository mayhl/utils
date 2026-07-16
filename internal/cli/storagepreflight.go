package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// storagePreflight surfaces how much a sync push will add to each destination filesystem
// against its disk quota, before the confirm, and warns — softly — when the push would not
// fit. Advisory only: it never blocks a sync and never errors; a failed show_storage or an
// unquota'd filesystem just drops the headroom figures. syncShared always reaches the
// cluster (the classify pass needs it), so this fetches show_usage's sibling live rather
// than caching — disk headroom moves with every write, so a stale number would mislead.
//
// The quota match is a plain path-prefix: show_storage reports each filesystem's root in
// the same symlink form resolveRemoteDir yields (e.g. Location "/p/work1" is a prefix of
// "/p/work1/<user>/<HomeRel>/..."), so no readlink/canonicalization is needed — and on a
// cluster whose scratch $WORKDIR carries no quota row at all, the push simply reports "no
// quota reported", which is the honest answer, not a failure.
func storagePreflight(node string, results []syncResult) {
	// Sum the new bytes each result adds; skip the ones that push nothing.
	type pending struct {
		dest  string
		bytes int64
	}
	var pend []pending
	for _, res := range results {
		if b := sumNewBytes(res); b > 0 {
			pend = append(pend, pending{dest: res.dest, bytes: b})
		}
	}
	if len(pend) == 0 {
		return
	}

	_, out, err := fetchSite(node, showStorageCmd)
	var rows []queue.StorageInfo
	if err == nil {
		rows = queue.ParseShowStorage(out)
	}

	// Group the push by the filesystem it lands on — several tiers can share one (sim and
	// processed both under $WORKDIR) — keyed by the matched quota row, or the bare mount
	// root when nothing matched.
	type group struct {
		label string
		row   *queue.StorageInfo
		bytes int64
	}
	order := []string{}
	byKey := map[string]*group{}
	for _, p := range pend {
		row, matched := matchStorageRow(p.dest, rows)
		key := fsRoot(p.dest)
		if matched {
			key = row.Location
		}
		g := byKey[key]
		if g == nil {
			g = &group{label: key}
			if matched {
				r := row
				g.row = &r
			}
			byKey[key] = g
			order = append(order, key)
		}
		g.bytes += p.bytes
	}

	for _, key := range order {
		g := byKey[key]
		add := render.HumanBytes(g.bytes)
		quotaKB, hasQuota := int64(0), false
		if g.row != nil {
			quotaKB, hasQuota = parseKB(g.row.DiskQuotaKB)
		}
		if !hasQuota {
			// Unmatched, or an unlimited/scrub-managed filesystem — no headroom to show.
			note := "no quota reported"
			if g.row != nil {
				note = "no quota"
			}
			render.Detail(fmt.Sprintf("storage: +%s → %s (%s)", add, g.label, note))
			continue
		}
		usedKB, _ := parseKB(g.row.DiskUsedKB)
		quotaB := quotaKB * 1024
		freeB := quotaB - usedKB*1024
		pushB := g.bytes
		pctBefore := float64(usedKB*1024) / float64(quotaB) * 100
		pctAfter := float64(usedKB*1024+pushB) / float64(quotaB) * 100
		render.Detail(fmt.Sprintf("storage: +%s → %s · %s free of %s · %.0f%% → %.0f%% after",
			add, g.label, render.HumanBytes(freeB), render.HumanBytes(quotaB), pctBefore, pctAfter))
		if pushB > freeB {
			render.Warn(fmt.Sprintf("push (%s) exceeds free space on %s (%s free) — the transfer may fail",
				add, g.label, render.HumanBytes(freeB)))
		}
	}
}

// sumNewBytes totals the local sizes of a result's new files. Best-effort — a file that
// can't be stat'd (a race, a broken symlink) is skipped, since the sum is only an advisory
// pre-flight figure, not the transfer itself.
func sumNewBytes(res syncResult) int64 {
	var total int64
	for _, rel := range res.newPaths {
		fi, err := os.Stat(filepath.Join(res.localAbs, rel))
		if err != nil {
			continue
		}
		total += fi.Size()
	}
	return total
}

// matchStorageRow picks the show_storage row whose Location is a path-prefix of dest,
// longest match winning (so "/p/work1" beats a stray "/p"). ok=false when no row's
// filesystem contains dest — a scratch mount with no quota row, or an unparseable table.
func matchStorageRow(dest string, rows []queue.StorageInfo) (queue.StorageInfo, bool) {
	var best queue.StorageInfo
	found := false
	for _, r := range rows {
		loc := strings.TrimRight(strings.TrimSpace(r.Location), "/")
		if loc == "" || !pathHasPrefix(dest, loc) {
			continue
		}
		if !found || len(loc) > len(strings.TrimRight(best.Location, "/")) {
			best, found = r, true
		}
	}
	return best, found
}

// pathHasPrefix reports whether prefix is dest itself or an ancestor directory of it —
// a component-boundary prefix, so "/p/work1" matches "/p/work1/u" but not "/p/work12/u".
func pathHasPrefix(dest, prefix string) bool {
	return dest == prefix || strings.HasPrefix(dest, prefix+"/")
}

// fsRoot is the bare mount label for an unmatched dest — the first two path components
// ("/p/work" from "/p/work/user/..."), enough to name the filesystem in the note.
func fsRoot(dest string) string {
	parts := strings.SplitN(strings.TrimPrefix(dest, "/"), "/", 3)
	if len(parts) >= 2 {
		return "/" + parts[0] + "/" + parts[1]
	}
	return dest
}

// parseKB reads a show_storage KB cell as an integer, tolerating thousands commas and
// treating a blank, "-", or non-positive value as "no quota" (unlimited / not reported).
func parseKB(s string) (int64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" || s == "-" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}
