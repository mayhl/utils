package doctor

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParsePluginOutput(t *testing.T) {
	cases := []struct {
		name, in                       string
		wantTitle, wantDetail, wantVer string
	}{
		{"title + body", "#TITLE: Providers\nrow one\nrow two\nall good", "Providers", "all good", "row one\nrow two"},
		{"title only", "#TITLE: Providers\n", "Providers", "", ""},
		{"multiline no title", "row one\ndetail", "", "detail", "row one"},
		{"single line", "just detail", "", "just detail", ""},
		{"trailing newline", "detail\n", "", "detail", ""},
		{"empty", "", "", "", ""},
	}
	for _, c := range cases {
		title, detail, ver := parsePluginOutput([]byte(c.in))
		if title != c.wantTitle || detail != c.wantDetail || ver != c.wantVer {
			t.Errorf("%s: got (%q,%q,%q), want (%q,%q,%q)", c.name, title, detail, ver, c.wantTitle, c.wantDetail, c.wantVer)
		}
	}
}

func TestLastLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"one", "one"},
		{"a\nb", "b"},
		{"a\nb\n", "b"},
	}
	for _, c := range cases {
		if got := lastLine(c.in); got != c.want {
			t.Errorf("lastLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCheckMise(t *testing.T) {
	writeExec := func(dir, name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// OK: mise resolvable on PATH.
	binDir := t.TempDir()
	misePath := writeExec(binDir, "mise")
	t.Setenv("PATH", binDir)
	if r := checkMise(); r.Status != OK || r.Detail != misePath {
		t.Errorf("on PATH: got %+v, want OK %s", r, misePath)
	}
	// Warn: not on PATH but present in ~/.local/bin.
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(localBin, "mise")
	t.Setenv("PATH", t.TempDir()) // no mise on PATH
	t.Setenv("HOME", home)
	if r := checkMise(); r.Status != Warn || !strings.Contains(r.Detail, "not on PATH") {
		t.Errorf("in ~/.local/bin: got %+v, want Warn not-on-PATH", r)
	}
	// Warn: absent entirely.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if r := checkMise(); r.Status != Warn || !strings.Contains(r.Detail, "not found") {
		t.Errorf("absent: got %+v, want Warn not-found", r)
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
