package queue

import (
	"strings"
	"testing"
)

func TestAdapterFor(t *testing.T) {
	if For("pbs").Name() != "pbs" || For("slurm").Name() != "slurm" {
		t.Fatal("For() returned the wrong adapter")
	}
	if For("bogus") != nil {
		t.Error("unknown scheduler should be nil")
	}
}

func TestAdapterCmds(t *testing.T) {
	pbsIDs := []string{"1284570.hpc1", "1284[7].hpc1"} // array brackets must stay quoted
	slurmIDs := []string{"9001", "9002"}
	cases := []struct{ name, got, want string }{
		{"pbs kill", For("pbs").KillCmd(pbsIDs), `qdel '1284570.hpc1' '1284[7].hpc1'`},
		{"slurm kill", For("slurm").KillCmd(slurmIDs), `scancel '9001' '9002'`},
		{"pbs hold", For("pbs").HoldCmd(pbsIDs, false), `qhold '1284570.hpc1' '1284[7].hpc1'`},
		{"pbs rls", For("pbs").HoldCmd(pbsIDs, true), `qrls '1284570.hpc1' '1284[7].hpc1'`},
		{"slurm hold", For("slurm").HoldCmd(slurmIDs, false), `scontrol hold '9001','9002'`},
		{"slurm rls", For("slurm").HoldCmd([]string{"9001"}, true), `scontrol release '9001'`},
		{"pbs detail", For("pbs").DetailCmd(pbsIDs), `qstat -f '1284570.hpc1' '1284[7].hpc1'`},
		{"slurm detail", For("slurm").DetailCmd(slurmIDs), `scontrol show job '9001','9002'`},
		{"pbs submit", For("pbs").SubmitCmd("run.pbs", SubmitOpts{Account: "PROJ1", Queue: "standard"}), `qsub -A 'PROJ1' -q 'standard' 'run.pbs'`},
		{"slurm submit", For("slurm").SubmitCmd("run.slurm", SubmitOpts{Account: "PROJ1"}), `sbatch -A 'PROJ1' 'run.slurm'`},
		{"pbs submit bare", For("pbs").SubmitCmd("run.pbs", SubmitOpts{}), `qsub 'run.pbs'`},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, c.got, c.want)
		}
	}
}

func TestAdapterDirectives(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"pbs both", strings.Join(For("pbs").Directives(SubmitOpts{Account: "PROJ1", Queue: "standard"}), "\n"), "#PBS -A PROJ1\n#PBS -q standard"},
		{"slurm both", strings.Join(For("slurm").Directives(SubmitOpts{Account: "PROJ1", Queue: "debug"}), "\n"), "#SBATCH -A PROJ1\n#SBATCH -p debug"},
		{"pbs bare", strings.Join(For("pbs").Directives(SubmitOpts{}), "\n"), ""},
		{"slurm account only", strings.Join(For("slurm").Directives(SubmitOpts{Account: "PROJ1"}), "\n"), "#SBATCH -A PROJ1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n got  %q\n want %q", c.name, c.got, c.want)
		}
	}
}
