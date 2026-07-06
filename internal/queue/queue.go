// Package queue is the scheduler read-side adapter: it turns a scheduler's native
// listing (PBS `qstat`, SLURM `squeue`) into a normalized []Job that the render
// layer draws as one house table, regardless of scheduler. Parsing is kept separate
// from fetching — the caller pipes in the listing — so it's unit-testable against
// fixtures without a live cluster, and the same normalized model feeds cross-cluster
// collate, `minfo`, and short-id remap later. Parse() sniffs the scheduler from the
// header, so a mixed site (PBS on some clusters, SLURM on others) is one model.
package queue

import (
	"encoding/json"
	"strings"
)

// State is the normalized job state, unifying scheduler-specific codes/words so the
// render + summary layers stay scheduler-agnostic.
type State int

const (
	Unknown State = iota
	Running
	Queued
	Held
	Exiting
	Complete
	Waiting
	Suspended
)

// MarshalJSON emits the normalized label ("running", …) rather than the enum int, so
// `--json` is a stable, scheduler-agnostic contract.
func (s State) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// String is the normalized state label (also the token the render layer keys on).
func (s State) String() string {
	switch s {
	case Running:
		return "running"
	case Queued:
		return "queued"
	case Held:
		return "held"
	case Exiting:
		return "exiting"
	case Complete:
		return "complete"
	case Waiting:
		return "waiting"
	case Suspended:
		return "suspended"
	default:
		return "unknown"
	}
}

// Job is one scheduler job, normalized across schedulers. Fields a given
// scheduler/format doesn't report are left empty ("" / zero State).
type Job struct {
	ID       string `json:"id"`       // full native id, e.g. "1284570.hpc1" (PBS) or "1284570" (SLURM)
	ShortID  string `json:"short_id"` // leading segment, e.g. "1284570"
	Name     string `json:"name"`
	User     string `json:"user"`
	Queue    string `json:"queue"`             // PBS queue / SLURM partition
	Nodes    string `json:"nodes"`             // node/chunk count (NDS); "" if not reported
	State    State  `json:"state"`             // normalized; marshals to its label string
	RawState string `json:"raw_state"`         // the scheduler's raw code, preserved for unknowns
	Elapsed  string `json:"elapsed"`           // elapsed / used time
	ReqWall  string `json:"walltime"`          // requested walltime; "" if not reported
	Reason   string `json:"reason"`            // SLURM NODELIST(REASON) — nodelist running, reason pending
	Cluster  string `json:"cluster,omitempty"` // set only by cross-cluster collate (--all); omitted otherwise
}

// PendingReason is the human reason a job is waiting: SLURM wraps a pending reason
// in parens ("(Priority)" → "Priority"), while a running job's field is a bare
// nodelist, which yields "". Lets the queue view explain why a job is stuck.
func (j Job) PendingReason() string {
	r := strings.TrimSpace(j.Reason)
	if len(r) >= 2 && r[0] == '(' && r[len(r)-1] == ')' {
		return r[1 : len(r)-1]
	}
	return ""
}

// Parse sniffs the format from the listing and dispatches to the right parser, so
// `mu hpc queue` works with any of them piped in: SLURM `squeue` headers lead with
// "JOBID", PBS `qstat` headers with "Job id"/"Job ID", and mu's own controlled
// fetch format is headerless with ≥8 pipe delimiters per line.
func Parse(out string) []Job {
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		up := strings.ToUpper(t)
		switch {
		case strings.HasPrefix(up, "JOBID"):
			return ParseSLURM(out)
		case strings.HasPrefix(up, "JOB ID"):
			return ParsePBS(out)
		case strings.Count(line, "|") >= 8:
			return ParseSLURMDelim(out)
		}
	}
	// No recognizable header — best effort: try PBS, fall back to SLURM.
	if j := ParsePBS(out); len(j) > 0 {
		return j
	}
	return ParseSLURM(out)
}

// pbsState maps a PBS job state code to the normalized State. Covers the full PBS
// Pro / OpenPBS set so a live code never leaks through as a raw letter.
func pbsState(code string) State {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "R", "B": // R running; B = array parent whose subjobs have begun
		return Running
	case "Q":
		return Queued
	case "H":
		return Held
	case "E":
		return Exiting
	case "C", "F", "X": // C completed (old PBS); F finished (PBS Pro); X subjob finished/expired
		return Complete
	case "W", "T", "M": // W waiting on exec time; T transiting; M moved to another server
		return Waiting
	case "S", "U": // S suspended; U suspended by keyboard/user activity (cycle-harvest)
		return Suspended
	default:
		return Unknown
	}
}

// slurmState maps a SLURM state code (ST column) or full word to the normalized
// State. Covers the documented squeue state set so a live code never leaks through
// as a raw abbreviation.
func slurmState(code string) State {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "R", "RUNNING", "RS", "RESIZING":
		return Running
	case "PD", "PENDING", "CF", "CONFIGURING", "RQ", "REQUEUED", "RF", "REQUEUE_FED":
		return Queued
	case "CG", "COMPLETING", "SO", "STAGE_OUT", "SI", "SIGNALING":
		return Exiting
	case "CD", "COMPLETED", "CA", "CANCELLED", "F", "FAILED", "TO", "TIMEOUT",
		"NF", "NODE_FAIL", "OOM", "OUT_OF_MEMORY", "BF", "BOOT_FAIL", "DL", "DEADLINE",
		"PR", "PREEMPTED", "RV", "REVOKED", "SE", "SPECIAL_EXIT":
		return Complete
	case "RD", "RESV_DEL_HOLD", "RH", "REQUEUE_HOLD":
		return Held
	case "S", "SUSPENDED", "ST", "STOPPED":
		return Suspended
	default:
		return Unknown
	}
}

// shortID is the leading segment of a PBS id ("1284570.hpc1" → "1284570"; an
// array id "1284[7].hpc1" → "1284[7]"). SLURM ids have no host suffix, so this is
// a no-op there. Falls back to the whole id.
func shortID(id string) string {
	if i := strings.IndexByte(id, '.'); i > 0 {
		return id[:i]
	}
	return id
}

// ParsePBS parses PBS `qstat` output — both the default 6-column format and the
// wide `-a`/`-u` "alternate" format — into normalized jobs. Format is detected from
// the header; non-data lines (server banners, the Req'd/Elap sub-header, the dashed
// rule, blanks) are skipped, and a data line that doesn't fit its format is dropped
// rather than erroring, so a format surprise degrades to fewer rows, not a crash.
func ParsePBS(out string) []Job {
	var jobs []Job
	wide, seenRule := false, false
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimRight(line, " \t\r")
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "job id") {
			// The wide alternate format's header carries NDS/Elap columns.
			wide = strings.Contains(t, "NDS") || strings.Contains(t, "Elap")
			seenRule = false
			continue
		}
		if strings.HasPrefix(trimmed, "---") {
			seenRule = true
			continue
		}
		if !seenRule {
			continue // pre-data noise (server banner, the Req'd/Req'd/Elap sub-header)
		}
		f := strings.Fields(t)
		var j Job
		if wide {
			// Job ID | Username | Queue | Jobname | SessID | NDS | TSK | Req'dMem | Req'dTime | S | Elap
			if len(f) < 11 {
				continue
			}
			j = Job{
				ID: f[0], User: f[1], Queue: f[2], Name: f[3],
				Nodes: f[5], ReqWall: f[8], State: pbsState(f[9]), RawState: f[9],
				Elapsed: f[10],
			}
		} else {
			// Job id | Name | User | Time Use | S | Queue
			if len(f) < 6 {
				continue
			}
			j = Job{
				ID: f[0], Name: f[1], User: f[2],
				Elapsed: f[3], State: pbsState(f[4]), RawState: f[4], Queue: f[5],
			}
		}
		j.ShortID = shortID(j.ID)
		jobs = append(jobs, j)
	}
	return jobs
}

// ParseSLURM parses SLURM `squeue` default output into normalized jobs. The header
// (JOBID PARTITION NAME USER ST TIME NODES NODELIST(REASON)) and blanks are skipped;
// a data line that doesn't fit is dropped rather than erroring.
func ParseSLURM(out string) []Job {
	var jobs []Job
	seenHeader := false
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(t), "JOBID") {
			seenHeader = true
			continue
		}
		if !seenHeader {
			continue
		}
		f := strings.Fields(t)
		// JOBID | PARTITION | NAME | USER | ST | TIME | NODES | NODELIST(REASON)
		if len(f) < 8 {
			continue
		}
		jobs = append(jobs, Job{
			ID: f[0], ShortID: f[0], Queue: f[1], Name: f[2], User: f[3],
			State: slurmState(f[4]), RawState: f[4], Elapsed: f[5], Nodes: f[6],
			// NODELIST(REASON) is a single token: a running job's nodelist is a
			// node-id sequence (nid00[123-126]) and SLURM space-filters the pending
			// reason (PascalCase / -/_), so every squeue column is one token → 8.
			Reason: f[7],
		})
	}
	return jobs
}

// ParseSLURMDelim parses mu's own controlled fetch format,
// `squeue -h -o "%i|%P|%j|%u|%t|%M|%l|%D|%R"`: a pipe delimiter (not whitespace)
// means no truncation or space-in-field ambiguity, and %l (TIME_LIMIT) adds the
// walltime the default squeue lacks. Fields:
// JOBID | PARTITION | NAME | USER | STATE | TIME(elapsed) | TIME_LIMIT | NODES | NODELIST(REASON).
func ParseSLURMDelim(out string) []Job {
	var jobs []Job
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		f := strings.Split(t, "|")
		if len(f) < 9 {
			continue
		}
		jobs = append(jobs, Job{
			ID: f[0], ShortID: f[0], Queue: f[1], Name: f[2], User: f[3],
			State: slurmState(f[4]), RawState: f[4], Elapsed: f[5], ReqWall: f[6],
			Nodes: f[7], Reason: f[8],
		})
	}
	return jobs
}
