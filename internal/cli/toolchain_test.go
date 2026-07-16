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
// Static prepend-paths only (shims need a per-user mise config to resolve, so a
// config-less consumer gets "No version is set"); MU_TOOLCHAIN is the module-provided
// marker the zsh MISE_ENV composition keys on.
func TestWriteModulefile(t *testing.T) {
	tc := &toolchain{prefix: t.TempDir() + "/tc"}
	bins := []string{
		tc.prefix + "/installs/neovim/0.12.4/bin",
		tc.prefix + "/installs/difftastic/latest",
	}
	if err := tc.writeModulefile(bins); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(tc.modulefilePath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"#%Module1.0",
		"prepend-path PATH " + bins[0],
		"prepend-path PATH " + bins[1],
		"setenv MU_TOOLCHAIN " + tc.prefix,
	} {
		if !strings.Contains(string(b), want+"\n") {
			t.Errorf("modulefile missing %q:\n%s", want, b)
		}
	}
	for _, banned := range []string{"shims", "MISE_DATA_DIR"} {
		if strings.Contains(string(b), banned) {
			t.Errorf("modulefile must not reference %q (consumer-side mise):\n%s", banned, b)
		}
	}
}

// TestDeployModuleFakeMise drives the shared-deploy plumbing (trust → install →
// bin-paths → modulefile) against a stub mise, so the sequencing and the bin-paths →
// prepend-path handoff are covered without a linux box or a real install.
func TestDeployModuleFakeMise(t *testing.T) {
	dir := t.TempDir()
	prefix := dir + "/tc"
	fake := dir + "/mise"
	script := "#!/bin/sh\n" +
		"echo \"$1\" >> " + dir + "/calls\n" +
		"[ \"$1\" = bin-paths ] || exit 0\n" +
		"echo \"$MISE_DATA_DIR/installs/neovim/0.12.4/bin\"\n" +
		"echo \"$MISE_DATA_DIR/installs/python/3.12/bin\"\n" +
		"echo \"$MISE_DATA_DIR/installs/difftastic/latest\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tc := &toolchain{prefix: prefix, module: true}
	if err := tc.deployModule(fake); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(dir + "/calls")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(calls), "trust\ninstall\nbin-paths\n"; got != want {
		t.Errorf("mise calls = %q, want %q", got, want)
	}
	b, err := os.ReadFile(tc.modulefilePath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"prepend-path PATH " + prefix + "/installs/neovim/0.12.4/bin",
		"prepend-path PATH " + prefix + "/installs/difftastic/latest",
		"setenv MU_TOOLCHAIN " + prefix,
	} {
		if !strings.Contains(string(b), want+"\n") {
			t.Errorf("modulefile missing %q:\n%s", want, b)
		}
	}
	// python is a backend runtime (pipx venv shebangs), never a PATH deliverable —
	// HPC sites hand out user pythons via their own modules.
	if strings.Contains(string(b), "/installs/python/") {
		t.Errorf("modulefile must not prepend-path the python backend:\n%s", b)
	}
}

// TestOverrideEnv pins replace-not-append semantics — a duplicate key appended after
// the original is undefined territory for getenv.
func TestOverrideEnv(t *testing.T) {
	got := overrideEnv([]string{"A=1", "MISE_ENV=fmt", "B=2"}, "MISE_ENV=hpc", "C=3")
	want := "A=1 B=2 MISE_ENV=hpc C=3"
	if s := strings.Join(got, " "); s != want {
		t.Errorf("overrideEnv = %q, want %q", s, want)
	}
}
