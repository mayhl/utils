package rsync

import "testing"

// --exclude-hidden must emit a single `--exclude .*` so dotfiles/dot-dirs are skipped.
func TestBuildArgsExcludeHidden(t *testing.T) {
	t.Setenv("MU_CONFIG_FILE", "") // isolate from any ambient config.toml

	args := BuildArgs("src", "dst", Opts{ExcludeHidden: true})
	if !hasPair(args, "--exclude", ".*") {
		t.Fatalf("--exclude .* not emitted: %v", args)
	}

	// Off by default.
	if hasPair(BuildArgs("src", "dst", Opts{}), "--exclude", ".*") {
		t.Fatal("--exclude .* emitted without ExcludeHidden")
	}
}

// A set Transport rides -e verbatim (an hpc.Session's RsyncTransport); empty falls back to the
// config-built ssh transport, so plain `mu cp` is unchanged.
func TestBuildArgsTransport(t *testing.T) {
	t.Setenv("MU_CONFIG_FILE", "") // isolate from any ambient config.toml

	custom := "ssh -x -S /tmp/mu-mux-1 -o ControlMaster=no"
	if !hasPair(BuildArgs("src", "dst", Opts{Transport: custom}), "-e", custom) {
		t.Fatalf("custom transport not passed to -e")
	}
	// Default (no Transport): -e is still emitted, with a non-empty value that isn't ours.
	args := BuildArgs("src", "dst", Opts{})
	found := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-e" {
			found = true
			if args[i+1] == "" || args[i+1] == custom {
				t.Fatalf("default transport wrong: %q", args[i+1])
			}
		}
	}
	if !found {
		t.Fatal("-e not emitted for the default transport")
	}
}

func hasPair(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}
