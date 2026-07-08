package queue

import "testing"

// Header + ruler are verbatim from `show_queues`; the data rows are synthesized, aligned
// to the ruler. E/R take Y / N / - (the real vocabulary). The rows exercise the flag cases
// we must tolerate: up (standard, Y Y), enabled-but-stopped (debug, Y N), a routing queue
// (route), a not-reported row (bare, - -), and disabled (frozen, N N). The leading blank +
// NODE INFORMATION section exercise the section anchoring.
const showQueuesSample = `
QUEUE INFORMATION:
                |---------- Limits ---------| |-------- Counts --------|
                   Maximum   Max   Min    Max  Jobs  Jobs  Cores   Cores  Queue
Queue Name        Walltime  Jobs Cores  Cores   Run  Pend    Run    Pend Typ E R
=============== ========== ===== ===== ====== ===== ===== ====== ======= =======
standard        168:00:00     50     1   4096    12     3   2048     512 Exe Y Y
debug            01:00:00     10     1    512     2     0    128       0 Exe Y N
route           168:00:00     50     1   4096     0     0      0       0 Rou Y Y
bare            168:00:00     50     1   4096     4     1    512     128 Exe - -
frozen          168:00:00     50     1   4096     0     0      0       0 Exe N N

NODE INFORMATION:
                     Nodes       Cores         Cores        Cores         Cores
Node Type          Available *  Per Node  =  Available -   Running  =      Free
===============   ==========   ==========   ==========   ==========   ==========
compute                  100          128        12800         6400         6400
`

func TestParseShowQueues(t *testing.T) {
	got := ParseShowQueues(showQueuesSample)
	if len(got) != 5 {
		t.Fatalf("want 5 queues, got %d: %+v", len(got), got)
	}
	q := got[0]
	want := QueueInfo{
		Name: "standard", MaxWalltime: "168:00:00", MaxJobs: "50", MinCores: "1",
		MaxCores: "4096", JobsRun: "12", JobsPend: "3", CoresRun: "2048", CoresPend: "512",
		Type: "Exe", Enabled: "Y", Running: "Y",
	}
	if q != want {
		t.Errorf("row0:\n got  %+v\n want %+v", q, want)
	}
	if got[1].Name != "debug" || got[1].Running != "N" { // enabled but stopped
		t.Errorf("row1 mismatch: %+v", got[1])
	}
	if got[2].Type != "Rou" { // routing queue — Typ distinguishes it from submittable
		t.Errorf("row2 want Type=Rou, got %+v", got[2])
	}
	// "-" flags must survive verbatim (not be coerced or misparsed from a limit).
	if b := got[3]; b.Name != "bare" || b.Type != "Exe" || b.Enabled != "-" || b.Running != "-" {
		t.Errorf("row3 want bare/Exe with - - flags, got %+v", b)
	}
	if f := got[4]; f.Enabled != "N" || f.Running != "N" { // disabled
		t.Errorf("row4 want frozen N N, got %+v", f)
	}
}
