package queue

import "testing"

const scontrolSample = `JobId=8359638 JobName=run_wave
   UserId=alice(30015) GroupId=alice(30015)
   Account=proj123 JobState=RUNNING Reason=None
   Partition=standard
   NumNodes=4 NumCPUs=96 NumTasks=96
   RunTime=06:14:52 TimeLimit=1-00:00:00
   SubmitTime=2026-07-05T17:40:00 StartTime=2026-07-06T00:00:00 EndTime=Unknown
   BatchHost=nid00045
   WorkDir=/p/work1/alice/run
   StdOut=/p/work1/alice/run/run.8359638.out
   StdErr=/p/work1/alice/run/run.8359638.err
   ExitCode=0:0
`

func TestParseDetailSLURM(t *testing.T) {
	d := ParseDetail("slurm", scontrolSample)
	if d.ID != "8359638" || d.ShortID != "8359638" || d.Name != "run_wave" {
		t.Errorf("id/name: %+v", d)
	}
	if d.User != "alice" { // UserId=alice(30015) → alice
		t.Errorf("user: %q", d.User)
	}
	if d.Account != "proj123" || d.Queue != "standard" || d.State != "running" {
		t.Errorf("account/queue/state: %+v", d)
	}
	if d.Nodes != "4" || d.Tasks != "96" || d.Elapsed != "06:14:52" || d.ReqWall != "1-00:00:00" {
		t.Errorf("resources: %+v", d)
	}
	if d.Submit != "2026-07-05T17:40:00" || d.Start != "2026-07-06T00:00:00" {
		t.Errorf("times: %+v", d)
	}
	if d.WorkDir != "/p/work1/alice/run" || d.StdOut != "/p/work1/alice/run/run.8359638.out" {
		t.Errorf("paths: %+v", d)
	}
	// Reason=None is a sentinel → blanked so the card omits it.
	if d.Reason != "" {
		t.Errorf("Reason None should blank, got %q", d.Reason)
	}
}

const qstatFSample = `Job Id: 1284570.hpc1
    Job_Name = run_wave
    Job_Owner = alice@hpc1
    job_state = R
    queue = standard
    Resource_List.nodect = 4
    Resource_List.ncpus = 96
    Resource_List.walltime = 24:00:00
    resources_used.walltime = 06:14:52
    ctime = Sat Jul  5 17:40:00 2026
    stime = Sun Jul  6 00:00:00 2026
    Output_Path = hpc1:/home/alice/very/long/path/that/qstat/wrapped/run.o12
	84570
    Error_Path = hpc1:/home/alice/run.e1284570
    Variable_List = PBS_O_HOME=/home/alice,PBS_O_WORKDIR=/p/work1/alice/simulat
	ions/funwave,PBS_O_PATH=/usr/bin
    exec_host = nid001/0*128+nid002/0*128
    exit_status = 0
`

func TestParseDetailPBS(t *testing.T) {
	d := ParseDetail("pbs", qstatFSample)
	if d.ID != "1284570.hpc1" || d.ShortID != "1284570" || d.Name != "run_wave" {
		t.Errorf("id/name: %+v", d)
	}
	if d.User != "alice" || d.State != "running" || d.Queue != "standard" { // Job_Owner alice@hpc1 → alice
		t.Errorf("user/state/queue: %+v", d)
	}
	if d.Nodes != "4" || d.Tasks != "96" || d.ReqWall != "24:00:00" || d.Elapsed != "06:14:52" {
		t.Errorf("resources: %+v", d)
	}
	// PBS times are human strings, kept verbatim.
	if d.Submit != "Sat Jul  5 17:40:00 2026" || d.Start != "Sun Jul  6 00:00:00 2026" {
		t.Errorf("times: %+v", d)
	}
	// Output_Path is host-stripped and line-unwrapped.
	if d.StdOut != "/home/alice/very/long/path/that/qstat/wrapped/run.o1284570" {
		t.Errorf("stdout: %q", d.StdOut)
	}
	if d.ExitStatus != "0" {
		t.Errorf("exit: %q", d.ExitStatus)
	}
	// PBS submit dir comes from Variable_List's PBS_O_WORKDIR (line-unwrapped).
	if d.WorkDir != "/p/work1/alice/simulations/funwave" {
		t.Errorf("workdir: %q", d.WorkDir)
	}
	// exec_host collapses to the first node — the tunnel target.
	if d.ExecHost != "nid001" {
		t.Errorf("exec_host: %q", d.ExecHost)
	}
}

func TestExecHostSLURM(t *testing.T) {
	if d := ParseDetail("slurm", scontrolSample); d.ExecHost != "nid00045" {
		t.Errorf("batchhost: %q", d.ExecHost)
	}
}

func TestParseSubmitID(t *testing.T) {
	cases := []struct{ scheduler, out, want string }{
		{"pbs", "1284575.sdb\n", "1284575.sdb"},
		{"pbs", "\nsome banner\n1284575.sdb\n", "1284575.sdb"},
		{"slurm", "Submitted batch job 8359640\n", "8359640"},
		{"slurm", "sbatch: error: invalid partition\n", ""},
		{"pbs", "qsub: script not found\n", ""},
	}
	for _, c := range cases {
		if got := ParseSubmitID(c.scheduler, c.out); got != c.want {
			t.Errorf("ParseSubmitID(%s, %q) = %q want %q", c.scheduler, c.out, got, c.want)
		}
	}
}

func TestOutputPath(t *testing.T) {
	if got := OutputPath("slurm", scontrolSample, false); got != "/p/work1/alice/run/run.8359638.out" {
		t.Errorf("slurm stdout: %q", got)
	}
	if got := OutputPath("slurm", scontrolSample, true); got != "/p/work1/alice/run/run.8359638.err" {
		t.Errorf("slurm stderr: %q", got)
	}
	if got := OutputPath("pbs", qstatFSample, true); got != "/home/alice/run.e1284570" {
		t.Errorf("pbs stderr: %q", got)
	}
	if got := OutputPath("slurm", "JobId=1 JobName=x", false); got != "" {
		t.Errorf("absent StdOut should be empty: %q", got)
	}
}

func TestParseDetailsMulti(t *testing.T) {
	// Two scontrol records back-to-back split into two details.
	blob := scontrolSample + "\nJobId=8359639 JobName=post_proc\n   JobState=PENDING Partition=standard\n"
	ds := ParseDetails("slurm", blob)
	if len(ds) != 2 || ds[0].ID != "8359638" || ds[1].ID != "8359639" {
		t.Fatalf("want 2 records 8359638/8359639, got %+v", ds)
	}
}
