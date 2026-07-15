package render

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// captureStderr runs f with os.Stderr redirected to a pipe and returns what it wrote. The tier
// helpers read os.Stderr at call time, so the swap is observed.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	f()
	_ = w.Close()
	os.Stderr = old
	var b bytes.Buffer
	_, _ = io.Copy(&b, r)
	return b.String()
}

func TestVerbosityPredicates(t *testing.T) {
	defer func() { Verbosity = LevelNormal }()
	cases := []struct {
		lvl              Level
		verbose, isQuiet bool
	}{
		{LevelQuiet, false, true},
		{LevelNormal, false, false},
		{LevelVerbose, true, false},
	}
	for _, c := range cases {
		Verbosity = c.lvl
		if IsVerbose() != c.verbose || IsQuiet() != c.isQuiet {
			t.Errorf("level %d: IsVerbose=%v IsQuiet=%v, want %v/%v", c.lvl, IsVerbose(), IsQuiet(), c.verbose, c.isQuiet)
		}
	}
}

func TestVerboseLineGating(t *testing.T) {
	defer func() { Verbosity = LevelNormal }()

	Verbosity = LevelNormal
	if out := captureStderr(t, func() { Verbose("chatter") }); out != "" {
		t.Errorf("Verbose at normal printed %q, want nothing", out)
	}
	Verbosity = LevelVerbose
	if out := captureStderr(t, func() { Verbose("chatter") }); !bytes.Contains([]byte(out), []byte("chatter")) {
		t.Errorf("Verbose at -v printed %q, want it to contain the message", out)
	}
}

func TestQuietSuppressesDetailAndInfo(t *testing.T) {
	defer func() { Verbosity = LevelNormal }()

	Verbosity = LevelNormal
	if out := captureStderr(t, func() { Detail("d") }); out == "" {
		t.Error("Detail at normal printed nothing, want the line")
	}
	Verbosity = LevelQuiet
	if out := captureStderr(t, func() { Detail("d") }); out != "" {
		t.Errorf("Detail at -q printed %q, want nothing", out)
	}
	if out := captureStderr(t, func() { Info("i") }); out != "" {
		t.Errorf("Info at -q printed %q, want nothing", out)
	}
}
