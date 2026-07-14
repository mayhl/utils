package cli

import (
	"strings"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/tomledit"
)

// TestSplitTOMLSections checks the inventory body and the named seam tables separate
// cleanly: seam content leaves the body, inventory stays.
func TestSplitTOMLSections(t *testing.T) {
	text := `hpc_user = "me"
fleet = ["a", "b"]

[ssh]
ossh = "/laptop/ossh"

[sshfs]
root = "/laptop/mnt"

[[cluster]]
name = "alpha"
scheduler = "slurm"
`
	rest, secs := tomledit.Split(text, localSeams...)
	if strings.Contains(rest, "ossh") || strings.Contains(rest, "/laptop/mnt") {
		t.Errorf("seam content leaked into body:\n%s", rest)
	}
	if !strings.Contains(rest, "hpc_user") || !strings.Contains(rest, "alpha") {
		t.Errorf("body lost inventory:\n%s", rest)
	}
	if !strings.Contains(secs["ssh"], "/laptop/ossh") || !strings.Contains(secs["sshfs"], "/laptop/mnt") {
		t.Errorf("seams not captured: %#v", secs)
	}
}

// TestAssembleKeepsTargetSeams is the core sync promise: shared inventory comes from
// this machine, but the target's [ssh]/[sshfs] survive and this machine's are dropped.
func TestAssembleKeepsTargetSeams(t *testing.T) {
	laptop := `hpc_user = "me"
[ssh]
ossh = "/laptop/ossh"
[[cluster]]
name = "alpha"
`
	target := `hpc_user = "old"
[ssh]
ossh = "/box/ossh"
[sshfs]
root = "/box/mnt"
`
	rest, _ := tomledit.Split(laptop, localSeams...)
	_, seams := tomledit.Split(target, localSeams...)
	merged := tomledit.Assemble(rest, seams, localSeams...)

	if !strings.Contains(merged, "alpha") {
		t.Error("lost this machine's inventory")
	}
	if strings.Contains(merged, "/laptop/ossh") {
		t.Errorf("propagated this machine's [ssh] seam:\n%s", merged)
	}
	if !strings.Contains(merged, "/box/ossh") || !strings.Contains(merged, "/box/mnt") {
		t.Errorf("did not preserve the target's seams:\n%s", merged)
	}
}
