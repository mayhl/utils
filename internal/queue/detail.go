package queue

import "strings"

// JobDetail is one job's full attributes, normalized across schedulers from `qstat -f`
// (PBS) / `scontrol show job` (SLURM) — the model behind the prettified `minfo` card and
// the source of a job's output paths for `mpeek`. A field a scheduler doesn't report is
// left "" (the card omits empty rows). Times are kept verbatim as the scheduler prints
// them (SLURM ISO 8601; PBS human "Wed Jul 6 00:00:00 2026") — the render layer formats.
type JobDetail struct {
	ID         string `json:"id"`
	ShortID    string `json:"short_id"`
	Name       string `json:"name"`
	User       string `json:"user"`
	Account    string `json:"account,omitempty"`
	Queue      string `json:"queue"`
	State      string `json:"state"`
	RawState   string `json:"raw_state"`
	Nodes      string `json:"nodes,omitempty"`
	Tasks      string `json:"tasks,omitempty"`
	Elapsed    string `json:"elapsed,omitempty"`
	ReqWall    string `json:"walltime,omitempty"`
	Submit     string `json:"submit,omitempty"`
	Start      string `json:"start,omitempty"`
	End        string `json:"end,omitempty"`
	WorkDir    string `json:"workdir,omitempty"`
	StdOut     string `json:"stdout,omitempty"`
	StdErr     string `json:"stderr,omitempty"`
	ExitStatus string `json:"exit_status,omitempty"`
	Reason     string `json:"reason,omitempty"`
	ExecHost   string `json:"exec_host,omitempty"` // first allocated node; "" until running
}

// ParseDetail turns raw scheduler detail into a JobDetail, dispatching on the configured
// scheduler. An unknown scheduler yields a zero JobDetail.
func ParseDetail(scheduler, raw string) JobDetail {
	switch scheduler {
	case "pbs":
		return parseQstatF(raw)
	case "slurm":
		return parseScontrol(raw)
	default:
		return JobDetail{}
	}
}

// ParseDetails splits a multi-record detail blob (several ids requested at once) into one
// JobDetail per job. Records are delimited by each scheduler's record header — PBS
// `qstat -f` "Job Id:" lines, SLURM `scontrol show job` blank-line-separated "JobId="
// blocks. A single job yields a one-element slice.
func ParseDetails(scheduler, raw string) []JobDetail {
	var out []JobDetail
	for _, rec := range splitDetailRecords(scheduler, raw) {
		d := ParseDetail(scheduler, rec)
		if d.ID != "" || d.Name != "" {
			out = append(out, d)
		}
	}
	return out
}

// splitDetailRecords cuts a detail blob into per-job record strings.
func splitDetailRecords(scheduler, raw string) []string {
	lines := strings.Split(raw, "\n")
	var recs []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			recs = append(recs, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, ln := range lines {
		isHeader := false
		switch scheduler {
		case "pbs":
			isHeader = strings.HasPrefix(ln, "Job Id:")
		case "slurm":
			isHeader = strings.HasPrefix(strings.TrimSpace(ln), "JobId=")
		}
		if isHeader {
			flush()
		}
		cur = append(cur, ln)
	}
	flush()
	if len(recs) == 0 {
		return []string{raw}
	}
	return recs
}

// parseScontrol reads `scontrol show job` — whitespace-separated key=value tokens.
func parseScontrol(raw string) JobDetail {
	d := JobDetail{
		ID:         slurmField(raw, "JobId"),
		Name:       slurmField(raw, "JobName"),
		Account:    skipNone(slurmField(raw, "Account")),
		Queue:      slurmField(raw, "Partition"),
		RawState:   slurmField(raw, "JobState"),
		Nodes:      slurmField(raw, "NumNodes"),
		Tasks:      slurmField(raw, "NumCPUs"),
		Elapsed:    slurmField(raw, "RunTime"),
		ReqWall:    slurmField(raw, "TimeLimit"),
		Submit:     slurmField(raw, "SubmitTime"),
		Start:      slurmField(raw, "StartTime"),
		End:        slurmField(raw, "EndTime"),
		WorkDir:    slurmField(raw, "WorkDir"),
		StdOut:     slurmField(raw, "StdOut"),
		StdErr:     slurmField(raw, "StdErr"),
		ExitStatus: skipNone(slurmField(raw, "ExitCode")),
		Reason:     skipNone(slurmField(raw, "Reason")),
		ExecHost:   skipNone(slurmField(raw, "BatchHost")), // the batch script's node
	}
	// UserId=alice(30015) → alice
	if u := slurmField(raw, "UserId"); u != "" {
		if i := strings.IndexByte(u, '('); i > 0 {
			u = u[:i]
		}
		d.User = u
	}
	d.State = slurmState(d.RawState).String()
	d.ShortID = shortID(d.ID)
	return d
}

// parseQstatF reads PBS `qstat -f` — indented "Key = value" attributes with tab-wrapped
// continuations, plus the "Job Id:" record header.
func parseQstatF(raw string) JobDetail {
	d := JobDetail{
		Name:       pbsAttr(raw, "Job_Name"),
		Account:    pbsAttr(raw, "Account_Name"),
		Queue:      pbsAttr(raw, "queue"),
		RawState:   pbsAttr(raw, "job_state"),
		Nodes:      pbsAttr(raw, "Resource_List.nodect"),
		Tasks:      pbsAttr(raw, "Resource_List.ncpus"),
		ReqWall:    pbsAttr(raw, "Resource_List.walltime"),
		Elapsed:    pbsAttr(raw, "resources_used.walltime"),
		Submit:     pbsAttr(raw, "ctime"),
		Start:      pbsAttr(raw, "stime"),
		End:        pbsAttr(raw, "mtime"),
		ExitStatus: pbsAttr(raw, "exit_status"),
		Reason:     pbsAttr(raw, "comment"),
		StdOut:     hostStrip(pbsAttr(raw, "Output_Path")),
		StdErr:     hostStrip(pbsAttr(raw, "Error_Path")),
	}
	// "Job Id: 1284570.hpc1" record header.
	for _, ln := range strings.Split(raw, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(ln), "Job Id:"); ok {
			d.ID = strings.TrimSpace(rest)
			break
		}
	}
	// Job_Owner = alice@host → alice
	if o := pbsAttr(raw, "Job_Owner"); o != "" {
		if i := strings.IndexByte(o, '@'); i > 0 {
			o = o[:i]
		}
		d.User = o
	}
	// PBS has no first-class workdir attribute — the submit dir rides in the
	// comma-separated Variable_List as PBS_O_WORKDIR.
	for _, kv := range strings.Split(pbsAttr(raw, "Variable_List"), ",") {
		if v, ok := strings.CutPrefix(kv, "PBS_O_WORKDIR="); ok {
			d.WorkDir = v
			break
		}
	}
	// exec_host = "nid001/0*128+nid002/0*128" → the first node carries the
	// tunnel-able services (rank 0 / the batch script's host).
	if h := pbsAttr(raw, "exec_host"); h != "" {
		d.ExecHost = strings.SplitN(h, "/", 2)[0]
	}
	d.State = pbsState(d.RawState).String()
	d.ShortID = shortID(d.ID)
	return d
}

// ParseSubmitID extracts the new job id from a submit command's stdout — PBS
// qsub prints the bare full id ("1284575.sdb"), SLURM sbatch a sentence
// ("Submitted batch job 8359640"). "" when nothing id-shaped came back.
func ParseSubmitID(scheduler, out string) string {
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(ln, "Submitted batch job "); ok && scheduler == "slurm" {
			return strings.TrimSpace(rest)
		}
		if scheduler == "pbs" && ln[0] >= '0' && ln[0] <= '9' {
			return ln
		}
	}
	return ""
}

// OutputPath returns a job's stdout (or stderr with wantErr) path from raw detail, for
// mpeek — a thin projection over ParseDetail so path extraction and the card share one
// parser.
func OutputPath(scheduler, raw string, wantErr bool) string {
	d := ParseDetail(scheduler, raw)
	if wantErr {
		return d.StdErr
	}
	return d.StdOut
}

// slurmField returns the value of a `key=value` token in scontrol's whitespace-separated
// output (values without embedded spaces, e.g. paths, times, counts), or "".
func slurmField(detail, key string) string {
	for _, f := range strings.Fields(detail) {
		if v, ok := strings.CutPrefix(f, key+"="); ok {
			return v
		}
	}
	return ""
}

// pbsAttr returns a `qstat -f` attribute value, first rejoining qstat's ~79-col line
// wrapping — a wrapped value continues on the next line, which begins with a TAB
// (attribute lines themselves are space-indented, so only a leading tab marks a
// continuation).
func pbsAttr(detail, key string) string {
	var joined []string
	for _, line := range strings.Split(detail, "\n") {
		if len(line) > 0 && line[0] == '\t' && len(joined) > 0 {
			joined[len(joined)-1] += strings.TrimLeft(line, " \t")
			continue
		}
		joined = append(joined, line)
	}
	for _, line := range joined {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, key); ok {
			rest = strings.TrimSpace(rest)
			if v, ok := strings.CutPrefix(rest, "="); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// hostStrip drops a leading "host:" from a PBS path value ("hpc1:/home/a/x" → "/home/a/x").
func hostStrip(v string) string {
	if i := strings.Index(v, ":"); i >= 0 {
		return v[i+1:]
	}
	return v
}

// skipNone blanks the scheduler's "no value" sentinels so the card omits the row.
func skipNone(v string) string {
	switch strings.TrimSpace(v) {
	case "None", "(null)", "":
		return ""
	}
	return v
}
