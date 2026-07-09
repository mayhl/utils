package queue

import (
	"strings"

	"github.com/mayhl/mayhl_utils/internal/shell"
)

// Adapter abstracts a batch scheduler (PBS / SLURM) behind one interface, so the queue
// read-side and `mu job` converge on a common model regardless of which scheduler a
// cluster runs. Each method builds the remote command string (run via hpc.RemoteExec);
// the impls own their binary names, id joining, and quoting — the rules differ per op
// (PBS space-joins everything; SLURM `scancel` space-joins but `scontrol` comma-joins).
type Adapter interface {
	Name() string
	KillCmd(ids []string) string                 // cancel jobs   (qdel / scancel)
	HoldCmd(ids []string, release bool) string   // hold / release (qhold·qrls / scontrol)
	DetailCmd(ids []string) string               // full detail   (qstat -f / scontrol show job)
	ListCmd(all bool, users, self string) string // live queue    (qstat -a / squeue -o …)
	HistCmd(all bool, users, self string) string // finished jobs (qstat -xa / sacct …)
	SubmitCmd(script string, o SubmitOpts) string
	Directives(o SubmitOpts) []string // header lines (#PBS / #SBATCH) for preview + templates
}

// SubmitOpts are the scheduler-neutral submit knobs; mu job sub populates them and the
// adapter maps each to the scheduler's flag. Empty fields fall through to the script's
// own directives / the scheduler default. (Grows as submit does: nodes, walltime, name…)
type SubmitOpts struct {
	Account string // allocation to charge (-A)
	Queue   string // queue / partition   (-q / -p)
}

// For returns the adapter for a scheduler name ("pbs" / "slurm"), or nil if unknown —
// callers treat nil the same as the old `switch` default ("" command).
func For(scheduler string) Adapter {
	switch scheduler {
	case "pbs":
		return pbsAdapter{}
	case "slurm":
		return slurmAdapter{}
	default:
		return nil
	}
}

func quoteJoin(ids []string, sep string) string {
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = shell.Quote(id)
	}
	return strings.Join(q, sep)
}

// pbsUserSel builds qstat's WHO selector: " -u <users>" for an explicit list, "" for all
// users, else the configured user (self), or "" if unset. Leading-space form to append
// after "qstat -a" / "qstat -xa"; self is the caller-resolved default user (queue stays
// config-free). Shared by the live (ListCmd) and history (HistCmd) PBS commands.
func pbsUserSel(all bool, users, self string) string {
	switch {
	case users != "":
		return " -u " + users
	case all:
		return ""
	default:
		if self != "" {
			return " -u " + self
		}
		return ""
	}
}

// ---- PBS (qsub/qstat/qdel/qhold) ------------------------------------------------------

type pbsAdapter struct{}

func (pbsAdapter) Name() string                { return "pbs" }
func (pbsAdapter) KillCmd(ids []string) string { return "qdel " + quoteJoin(ids, " ") }

func (pbsAdapter) HoldCmd(ids []string, release bool) string {
	bin := "qhold"
	if release {
		bin = "qrls"
	}
	return bin + " " + quoteJoin(ids, " ")
}

func (pbsAdapter) DetailCmd(ids []string) string { return "qstat -f " + quoteJoin(ids, " ") }

func (pbsAdapter) ListCmd(all bool, users, self string) string {
	return "qstat -a" + pbsUserSel(all, users, self)
}

func (pbsAdapter) HistCmd(all bool, users, self string) string {
	return "qstat -xa" + pbsUserSel(all, users, self)
}

func (pbsAdapter) SubmitCmd(script string, o SubmitOpts) string {
	cmd := "qsub"
	if o.Account != "" {
		cmd += " -A " + shell.Quote(o.Account)
	}
	if o.Queue != "" {
		cmd += " -q " + shell.Quote(o.Queue)
	}
	return cmd + " " + shell.Quote(script)
}

// Directives renders the #PBS header lines for preview/templates (display, not exec —
// unquoted). Empty opts yield no lines: the script's own directives / defaults apply.
func (pbsAdapter) Directives(o SubmitOpts) []string {
	var d []string
	if o.Account != "" {
		d = append(d, "#PBS -A "+o.Account)
	}
	if o.Queue != "" {
		d = append(d, "#PBS -q "+o.Queue)
	}
	return d
}

// ---- SLURM (sbatch/squeue/scancel/scontrol) -------------------------------------------

type slurmAdapter struct{}

func (slurmAdapter) Name() string                { return "slurm" }
func (slurmAdapter) KillCmd(ids []string) string { return "scancel " + quoteJoin(ids, " ") }

func (slurmAdapter) HoldCmd(ids []string, release bool) string {
	verb := "hold"
	if release {
		verb = "release"
	}
	return "scontrol " + verb + " " + quoteJoin(ids, ",")
}

func (slurmAdapter) DetailCmd(ids []string) string { return "scontrol show job " + quoteJoin(ids, ",") }

func (slurmAdapter) ListCmd(all bool, users, self string) string {
	sel := "--me " // default: just your jobs
	switch {
	case users != "":
		sel = "-u " + users + " "
	case all:
		sel = ""
	}
	return `squeue -h ` + sel + `-o "%i|%P|%j|%u|%t|%M|%l|%D|%R|%S"`
}

func (slurmAdapter) HistCmd(all bool, users, self string) string {
	sel := "" // sacct has no --me, so "just you" names the configured user explicitly
	switch {
	case users != "":
		sel = "-u " + users + " "
	case all:
		sel = "-a "
	default:
		if self != "" {
			sel = "-u " + self + " "
		}
	}
	return `sacct -X -n -p ` + sel + `-o JobIDRaw,JobName,User,Partition,State,Elapsed,Timelimit,NNodes,Submit,Start,End`
}

func (slurmAdapter) SubmitCmd(script string, o SubmitOpts) string {
	cmd := "sbatch"
	if o.Account != "" {
		cmd += " -A " + shell.Quote(o.Account)
	}
	if o.Queue != "" {
		cmd += " -p " + shell.Quote(o.Queue)
	}
	return cmd + " " + shell.Quote(script)
}

// Directives renders the #SBATCH header lines for preview/templates (display, not exec —
// unquoted). Empty opts yield no lines: the script's own directives / defaults apply.
func (slurmAdapter) Directives(o SubmitOpts) []string {
	var d []string
	if o.Account != "" {
		d = append(d, "#SBATCH -A "+o.Account)
	}
	if o.Queue != "" {
		d = append(d, "#SBATCH -p "+o.Queue)
	}
	return d
}
