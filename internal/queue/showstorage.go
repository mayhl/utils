package queue

import "strings"

// StorageInfo is one row of `show_storage`: a filesystem's disk and file usage vs quota
// for the user. Fields stay strings so blank/unlimited quotas and non-numeric markers
// survive verbatim (like QueueInfo); callers convert as needed. Disk figures are KB —
// mu formats human units itself rather than passing -h, so percent-used stays exact and
// the parser only ever sees integers. Lives here with ParseShowQueues for the shared
// ruler-table machinery; show_storage is a sibling site command of show_queues.
type StorageInfo struct {
	System      string
	Location    string
	DiskUsedKB  string
	DiskQuotaKB string
	FilesUsed   string
	FilesQuota  string
}

// ParseShowStorage parses the `show_storage` table. Unlike show_queues there's no section
// title, and the banner can contain its own all-`=` divider lines — so it anchors on the
// header line (contains "Disk Usage"), then the `=`-ruler under it (two-space separated
// runs, one per column), and reads rows until a blank line / EOF.
func ParseShowStorage(text string) []StorageInfo {
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && !strings.Contains(lines[i], "Disk Usage") {
		i++
	}
	for i < len(lines) && !isRuler(lines[i]) {
		i++
	}
	if i >= len(lines) {
		return nil
	}
	starts := columnStarts(lines[i])
	if len(starts) < 6 {
		return nil
	}
	var rows []StorageInfo
	for i++; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		c := sliceCols(lines[i], starts)
		rows = append(rows, StorageInfo{
			System: c[0], Location: c[1],
			DiskUsedKB: c[2], DiskQuotaKB: c[3],
			FilesUsed: c[4], FilesQuota: c[5],
		})
	}
	return rows
}
