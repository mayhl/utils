package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlugins(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("a-ok", "echo all good; exit 0", 0o755)
	write("b-warn", "echo heads up; exit 2", 0o755)
	write("c-fail", "echo broken; exit 1", 0o755)
	write("d-noexec", "echo ignored; exit 1", 0o644) // not executable → skipped
	t.Setenv("MU_CHECKS_DIR", dir)

	got := plugins()
	want := []Result{
		{Section: "checks", Name: "a-ok", Status: OK, Detail: "all good"},
		{Section: "checks", Name: "b-warn", Status: Warn, Detail: "heads up"},
		{Section: "checks", Name: "c-fail", Status: Fail, Detail: "broken"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want { // sorted by name; d-noexec absent
		if got[i] != w {
			t.Errorf("result[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestPluginsSubdirSection(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "providers")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "mise-owner"), []byte("#!/bin/sh\necho ok\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CHECKS_DIR", root)

	got := plugins()
	if len(got) != 1 || got[0].Section != "providers" || got[0].Name != "mise-owner" {
		t.Fatalf("subdir plugin should have Section=providers, got %+v", got)
	}
}

func TestPluginsNoDir(t *testing.T) {
	t.Setenv("MU_CHECKS_DIR", filepath.Join(t.TempDir(), "does-not-exist"))
	if got := plugins(); got != nil {
		t.Errorf("missing checks dir should yield nil, got %+v", got)
	}
}

func TestWorst(t *testing.T) {
	cases := []struct {
		in   []Result
		want Status
	}{
		{nil, OK},
		{[]Result{{Status: OK}, {Status: OK}}, OK},
		{[]Result{{Status: OK}, {Status: Warn}}, Warn},
		{[]Result{{Status: Warn}, {Status: Fail}, {Status: OK}}, Fail},
	}
	for i, c := range cases {
		if got := worst(c.in); got != c.want {
			t.Errorf("case %d: worst = %v, want %v", i, got, c.want)
		}
	}
}
