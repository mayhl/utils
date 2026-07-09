package cli

import "testing"

// TestModulefilePath pins the shared-prefix modulefile location — the assertion the
// sandbox TestToolchainDryRun used to guard, now covered locally (no box needed).
func TestModulefilePath(t *testing.T) {
	tc := &toolchain{prefix: "/opt/mu"}
	if got, want := tc.modulefilePath(), "/opt/mu/modulefiles/mu-toolchain"; got != want {
		t.Errorf("modulefilePath = %q, want %q", got, want)
	}
}
