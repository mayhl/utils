package cli

import "testing"

func TestCancelCmd(t *testing.T) {
	if got := cancelCmd("pbs", []string{"1284570.hpc1", "1284571.hpc1"}); got != `qdel '1284570.hpc1' '1284571.hpc1'` {
		t.Errorf("pbs batch: %q", got)
	}
	if got := cancelCmd("slurm", []string{"9001", "9002"}); got != `scancel '9001' '9002'` {
		t.Errorf("slurm batch: %q", got)
	}
	// PBS array brackets must be quoted so the remote shell doesn't glob-expand them.
	if got := cancelCmd("pbs", []string{"1284[7].hpc1"}); got != `qdel '1284[7].hpc1'` {
		t.Errorf("array id quoting: %q", got)
	}
	if got := cancelCmd("", []string{"1"}); got != "" {
		t.Errorf("unknown scheduler should yield empty cmd, got %q", got)
	}
}
