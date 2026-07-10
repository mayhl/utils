package queue

import "testing"

// Header + ruler are verbatim from `show_storage` (two-space separated `=` runs, single
// header line); the banner exercises the anchoring hazard — it carries its own all-`=`
// divider that must NOT be taken for the column ruler. Rows exercise: a normal quota'd
// fs, a big one, and an empty one with zero usage.
const showStorageSample = `
===============================================================
  Storage usage for tester — reported hourly, not live.
===============================================================

System     Location                  Disk Usage (KB)  Disk Quota (KB)  File Usage  File Quota
=========  ========================  ===============  ===============  ==========  ==========
hpc1       /p/home/tester                   52428800        104857600       41250      250000
hpc1       /p/work1/tester                9663676416      21474836480      182340     4000000
hpc1       /p/cwfs/tester                          0       1073741824           0     1000000
`

func TestParseShowStorage(t *testing.T) {
	got := ParseShowStorage(showStorageSample)
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(got), got)
	}
	want := StorageInfo{
		System: "hpc1", Location: "/p/home/tester",
		DiskUsedKB: "52428800", DiskQuotaKB: "104857600",
		FilesUsed: "41250", FilesQuota: "250000",
	}
	if got[0] != want {
		t.Errorf("row0:\n got  %+v\n want %+v", got[0], want)
	}
	if got[1].Location != "/p/work1/tester" || got[1].DiskQuotaKB != "21474836480" {
		t.Errorf("row1 mismatch: %+v", got[1])
	}
	if got[2].DiskUsedKB != "0" || got[2].FilesUsed != "0" {
		t.Errorf("row2 zero-usage mismatch: %+v", got[2])
	}
}

func TestParseShowStorageNoTable(t *testing.T) {
	if got := ParseShowStorage("=====\nbanner only, no table\n=====\n"); got != nil {
		t.Errorf("want nil on bannerless input, got %+v", got)
	}
}
