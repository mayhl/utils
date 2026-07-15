package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStagedPath(t *testing.T) {
	if got, want := stagedPath("3f9a"), "~/.local/state/mayhl_utils/jobs/3f9a.sh"; got != want {
		t.Errorf("stagedPath = %q, want %q", got, want)
	}
}

func TestIsLocalScript(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "serve.sh")
	if err := os.WriteFile(file, []byte("#!/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"existing file", file, true},
		{"empty", "", false},
		{"a directory", dir, false},
		{"only on the cluster", "~/serve.sh", false},
		{"absolute remote", "/p/home/u/serve.sh", false},
	}
	for _, c := range cases {
		if got := isLocalScript(c.path); got != c.want {
			t.Errorf("%s: isLocalScript(%q) = %v, want %v", c.name, c.path, got, c.want)
		}
	}
}

func TestWriteStaged(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "serve.sh")
	body := "#!/bin/bash\necho hi 'there'\n" // a single quote to exercise the quoting
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var ran string
	run := func(c string) (string, error) { ran = c; return "", nil }

	got, err := writeStaged(run, file, "3f9a")
	if err != nil {
		t.Fatal(err)
	}
	if want := "~/.local/state/mayhl_utils/jobs/3f9a.sh"; got != want {
		t.Errorf("returned path = %q, want %q", got, want)
	}
	// The remote command must resolve $HOME (not a quoted ~), create the dir, write the exact
	// body, and mark it executable — all under one command over the held connection.
	for _, want := range []string{
		`mkdir -p "$HOME/.local/state/mayhl_utils/jobs"`,
		`> "$HOME/.local/state/mayhl_utils/jobs/3f9a.sh"`,
		`chmod +x "$HOME/.local/state/mayhl_utils/jobs/3f9a.sh"`,
		`echo hi ` + `'\''there'\''`, // the body's single quote, escaped inside the printf arg
	} {
		if !strings.Contains(ran, want) {
			t.Errorf("staged command missing %q\ngot: %s", want, ran)
		}
	}
}

func TestWriteStagedMissingFile(t *testing.T) {
	run := func(string) (string, error) {
		t.Fatal("run should not be called for a missing local file")
		return "", nil
	}
	if _, err := writeStaged(run, filepath.Join(t.TempDir(), "nope.sh"), "3f9a"); err == nil {
		t.Error("expected an error for a missing local script")
	}
}
