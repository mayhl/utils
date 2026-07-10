package render

import (
	"strings"
	"testing"
)

func storageHeaders(rows []StorageRow) string {
	cols := planStorageCols(rows)
	h := make([]string, len(cols))
	for i, c := range cols {
		h[i] = c.header
	}
	return strings.Join(h, " ")
}

func TestPlanStorageCols(t *testing.T) {
	full := StorageRow{
		Location: "/p/home/u", DiskUsed: "50.0GB", DiskQuota: "100.0GB", DiskPct: "50",
		FilesUsed: "41250", FilesQuota: "250.0k", FilesPct: "17",
	}
	noFileQuota := full
	noFileQuota.FilesQuota, noFileQuota.FilesPct = "0", ""
	collated := full
	collated.System = "hpc1"

	cases := []struct {
		name string
		rows []StorageRow
		want string
	}{
		{"full", []StorageRow{full}, "Location Used Quota Use% Files FileQuota File%"},
		// The ask: a file-quota column that is all 0/blank is noise — drop the pair.
		{"no file quotas", []StorageRow{noFileQuota}, "Location Used Quota Use% Files"},
		{
			"no quotas at all",
			[]StorageRow{{Location: "/p", DiskUsed: "1.0GB", DiskQuota: "0B", FilesUsed: "5"}},
			"Location Used Files",
		},
		// One row with a real quota keeps the pair for all.
		{"mixed file quotas", []StorageRow{noFileQuota, full}, "Location Used Quota Use% Files FileQuota File%"},
		{"collate adds System", []StorageRow{collated}, "System Location Used Quota Use% Files FileQuota File%"},
	}
	for _, c := range cases {
		if got := storageHeaders(c.rows); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
	}
}
