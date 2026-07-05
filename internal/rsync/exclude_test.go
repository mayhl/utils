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

func hasPair(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}
