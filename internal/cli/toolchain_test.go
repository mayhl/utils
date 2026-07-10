package cli

import (
	"os"
	"strings"
	"testing"
)

// TestModulefilePath pins the shared-prefix modulefile location — the assertion the
// sandbox TestToolchainDryRun used to guard, now covered locally (no box needed).
func TestModulefilePath(t *testing.T) {
	tc := &toolchain{prefix: "/opt/mu"}
	if got, want := tc.modulefilePath(), "/opt/mu/modulefiles/mu-toolchain"; got != want {
		t.Errorf("modulefilePath = %q, want %q", got, want)
	}
}

// TestWriteModulefile pins the modulefile body — the sandbox TestToolchainModuleGuard
// replays these exact bytes on the box, so a drift here means updating that stub too.
// MU_TOOLCHAIN is the module-provided marker the zsh MISE_ENV composition keys on.
func TestWriteModulefile(t *testing.T) {
	tc := &toolchain{prefix: t.TempDir() + "/tc"}
	if err := tc.writeModulefile(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(tc.modulefilePath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"#%Module1.0",
		"prepend-path PATH " + tc.prefix + "/shims",
		"setenv MISE_DATA_DIR " + tc.prefix,
		"setenv MU_TOOLCHAIN " + tc.prefix,
	} {
		if !strings.Contains(string(b), want+"\n") {
			t.Errorf("modulefile missing %q:\n%s", want, b)
		}
	}
}
