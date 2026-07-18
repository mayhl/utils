package cli

import "testing"

// TestGuardPath locks what the harness refuses in a sent command: an absolute path, a home
// reference, or a parent-directory escape — and what it lets through (relative paths, flags,
// values that merely CONTAIN a dot).
func TestGuardPath(t *testing.T) {
	refused := map[string]string{
		"cat /etc/hostname":       "/etc/hostname",
		"ls ~/repos":              "~/repos",
		"ls ..":                   "..",
		"cat ../secret":           "../secret",
		"cp x sub/../../../out":   "sub/../../../out",
		"rm build/..":             "build/..",
		"make -j && cat /tmp/log": "/tmp/log",
	}
	for cmd, want := range refused {
		if got := guardPath(cmd); got != want {
			t.Errorf("guardPath(%q) = %q; want %q", cmd, got, want)
		}
	}
	clean := []string{
		"make",
		"./run_tests.sh --fast",
		"echo host=$(hostname)",
		"ls build/",
		"grep -n foo src/main.f90",
		"cmake -DBUILD=on .", // a bare '.' is the anchor itself, not an escape
	}
	for _, cmd := range clean {
		if got := guardPath(cmd); got != "" {
			t.Errorf("guardPath(%q) = %q; want clean", cmd, got)
		}
	}
}

// TestHarnessSlice checks the output is carved from between the TYPED command line (carrying the
// literal $?) and the SENTINEL output line (carrying the exit code) — even when other pane text,
// including the anchored wrapper and the prompt, surrounds it.
func TestHarnessSlice(t *testing.T) {
	tag := "__MUH_abc123def456"
	pane := "" +
		"node42:~$ ( cd '/p/home/u' && { hostname ; } ) ; echo \"" + tag + "__$?__\"\n" +
		"wheat-r11-cp43a\n" +
		tag + "__0__\n" +
		"node42:~$ \n"
	if got, want := harnessSlice(pane, tag), "wheat-r11-cp43a\n"; got != want {
		t.Errorf("harnessSlice single line = %q; want %q", got, want)
	}

	multi := "" +
		"prompt$ ( cd '/w' && { seq 3 ; } ) ; echo \"" + tag + "__$?__\"\n" +
		"1\n2\n3\n" +
		tag + "__0__\n"
	if got, want := harnessSlice(multi, tag), "1\n2\n3\n"; got != want {
		t.Errorf("harnessSlice multi = %q; want %q", got, want)
	}

	// A command with no output between the typed line and the sentinel yields an empty slice.
	empty := "prompt$ ( cd '/w' && { true ; } ) ; echo \"" + tag + "__$?__\"\n" + tag + "__0__\n"
	if got := harnessSlice(empty, tag); got != "" {
		t.Errorf("harnessSlice empty = %q; want \"\"", got)
	}
}
