package queue

import (
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParsePBSWide(t *testing.T) {
	jobs := ParsePBS(load(t, "qstat_u.txt"))
	if len(jobs) != 4 {
		t.Fatalf("want 4 jobs, got %d: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.ShortID != "1284570" || j.Name != "run_wave" || j.Queue != "standard" ||
		j.Nodes != "4" || j.State != Running || j.ReqWall != "24:00" || j.Elapsed != "06:14" {
		t.Errorf("job0 mismatch: %+v", j)
	}
	if jobs[1].State != Queued || jobs[1].Elapsed != "--" {
		t.Errorf("job1 (queued) mismatch: %+v", jobs[1])
	}
	if jobs[2].State != Held {
		t.Errorf("job2 want Held, got %v", jobs[2].State)
	}
}

func TestParsePBSNarrow(t *testing.T) {
	jobs := ParsePBS(load(t, "qstat.txt"))
	if len(jobs) != 3 {
		t.Fatalf("want 3 jobs, got %d: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.ShortID != "1284570" || j.Name != "run_wave" || j.User != "alice" ||
		j.State != Running || j.Queue != "standard" || j.Elapsed != "06:14:52" || j.ReqWall != "" {
		t.Errorf("job0 mismatch: %+v", j)
	}
	if jobs[1].State != Queued {
		t.Errorf("job1 want Queued, got %v", jobs[1].State)
	}
}

func TestParsePBSEmpty(t *testing.T) {
	if jobs := ParsePBS(load(t, "empty.txt")); len(jobs) != 0 {
		t.Errorf("want 0 jobs, got %d: %+v", len(jobs), jobs)
	}
}

func TestParseSLURM(t *testing.T) {
	jobs := ParseSLURM(load(t, "squeue.txt"))
	if len(jobs) != 4 {
		t.Fatalf("want 4 jobs, got %d: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.ShortID != "1284570" || j.Name != "run_wave" || j.Queue != "standard" ||
		j.User != "alice" || j.Nodes != "4" || j.State != Running || j.Elapsed != "6:14:32" {
		t.Errorf("job0 mismatch: %+v", j)
	}
	if jobs[1].State != Queued || jobs[1].Reason != "(Priority)" {
		t.Errorf("job1 (pending) mismatch: %+v", jobs[1])
	}
	if jobs[3].State != Exiting { // CG = completing
		t.Errorf("job3 want Exiting (CG), got %v", jobs[3].State)
	}
}

func TestParseSLURMDelim(t *testing.T) {
	jobs := ParseSLURMDelim(load(t, "squeue_delim.txt"))
	if len(jobs) != 3 {
		t.Fatalf("want 3 jobs, got %d: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.ID != "1284570" || j.Queue != "standard" || j.Name != "run_wave" ||
		j.State != Running || j.Elapsed != "6:14:32" || j.ReqWall != "1-00:00:00" || j.Nodes != "4" ||
		j.Start != "2026-07-06T00:00:00" {
		t.Errorf("job0 mismatch: %+v", j)
	}
	if jobs[1].State != Queued || jobs[1].ReqWall != "2:00:00" || jobs[1].PendingReason() != "Priority" ||
		jobs[1].Start != "2026-07-07T08:00:00" { // pending job carries the backfill estimate
		t.Errorf("job1 (pending) mismatch: %+v", jobs[1])
	}
}

func TestParseSacct(t *testing.T) {
	jobs := ParseSacct(load(t, "sacct.txt"))
	if len(jobs) != 4 {
		t.Fatalf("want 4 jobs, got %d: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.ShortID != "1284570" || j.Name != "run_wave" || j.User != "alice" || j.Queue != "standard" ||
		j.State != Complete || j.Elapsed != "06:14:52" || j.ReqWall != "24:00:00" || j.Nodes != "4" ||
		j.Submit != "2026-07-05T17:40:00" || j.Start != "2026-07-06T00:00:00" || j.End != "2026-07-06T06:14:52" {
		t.Errorf("job0 mismatch: %+v", j)
	}
	// "CANCELLED by <uid>" maps on its first token; the raw is preserved.
	if jobs[1].State != Complete || jobs[1].RawState != "CANCELLED by 30015" {
		t.Errorf("job1 (cancelled) mismatch: %+v", jobs[1])
	}
	if jobs[2].State != Complete { // TIMEOUT normalizes to Complete
		t.Errorf("job2 want Complete (TIMEOUT), got %v", jobs[2].State)
	}
	// An array id (12345_N) has no host suffix, so ShortID is the whole id.
	if jobs[3].ShortID != "1284580_3" || jobs[3].State != Complete {
		t.Errorf("job3 (array/failed) mismatch: %+v", jobs[3])
	}
}

// TestParseSLURMDelimNoStart: an older 9-field listing (pre-%S) still parses; the
// absent Start is simply empty.
func TestParseSLURMDelimNoStart(t *testing.T) {
	jobs := ParseSLURMDelim("1284570|standard|run_wave|alice|R|6:14:32|1-00:00:00|4|nid00[123-126]\n")
	if len(jobs) != 1 || jobs[0].ID != "1284570" || jobs[0].Start != "" {
		t.Errorf("9-field back-compat mismatch: %+v", jobs)
	}
}

func TestParseAutoDetect(t *testing.T) {
	if got := Parse(load(t, "squeue.txt")); len(got) != 4 || got[0].Queue != "standard" {
		t.Errorf("SLURM auto-detect: %+v", got)
	}
	if got := Parse(load(t, "qstat_u.txt")); len(got) != 4 || got[0].Nodes != "4" {
		t.Errorf("PBS auto-detect: %+v", got)
	}
}

func TestPendingReason(t *testing.T) {
	if got := (Job{Reason: "(QOSGrpNodeLimit)"}).PendingReason(); got != "QOSGrpNodeLimit" {
		t.Errorf("parenthesized reason = %q", got)
	}
	if got := (Job{Reason: "nid00[123-126]"}).PendingReason(); got != "" {
		t.Errorf("bare nodelist should yield empty, got %q", got)
	}
	if got := (Job{Reason: ""}).PendingReason(); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestPBSStateMap(t *testing.T) {
	cases := map[string]State{
		"R": Running, "B": Running,
		"Q": Queued,
		"H": Held,
		"E": Exiting,
		"C": Complete, "F": Complete, "X": Complete,
		"W": Waiting, "T": Waiting, "M": Waiting,
		"S": Suspended, "U": Suspended,
		" r ": Running, // trimmed + case-folded
		"Z":   Unknown, // unrecognized falls through
	}
	for code, want := range cases {
		if got := pbsState(code); got != want {
			t.Errorf("pbsState(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestSLURMStateMap(t *testing.T) {
	cases := map[string]State{
		"R": Running, "RS": Running, "RESIZING": Running,
		"PD": Queued, "CF": Queued, "RQ": Queued, "REQUEUE_FED": Queued,
		"CG": Exiting, "SO": Exiting, "SIGNALING": Exiting,
		"CD": Complete, "CA": Complete, "F": Complete, "TO": Complete,
		"NF": Complete, "OOM": Complete, "BF": Complete, "DL": Complete,
		"PR": Complete, "RV": Complete, "SE": Complete,
		"RD": Held, "REQUEUE_HOLD": Held,
		"S": Suspended, "ST": Suspended,
		"pending": Queued,  // case-folded full word
		"ZZ":      Unknown, // unrecognized falls through
	}
	for code, want := range cases {
		if got := slurmState(code); got != want {
			t.Errorf("slurmState(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("1284[7].hpc1"); got != "1284[7]" {
		t.Errorf("array id = %q", got)
	}
	if got := shortID("nohost"); got != "nohost" {
		t.Errorf("no-dot id = %q", got)
	}
}
