package render

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain keeps any emitter call in this package from touching the real
// ~/.cache/mayhl_utils/framework.log; individual tests override per-case.
func TestMain(m *testing.M) {
	if os.Getenv("MU_LOG_FILE") == "" {
		os.Setenv("MU_LOG_FILE", filepath.Join(os.TempDir(), "mu-render-test-default.log"))
	}
	os.Exit(m.Run())
}

func TestFrameworkLogFormat(t *testing.T) {
	logf := filepath.Join(t.TempDir(), "framework.log")
	t.Setenv("MU_LOG_FILE", logf)
	ResetLoggerForTest()

	Info("hello")           // default scope "mu"
	Scoped("cp").OK("done") // scoped

	got := readFile(t, logf)
	// [ts] [level(pad5)] [scope] msg
	for _, want := range []string{"[INFO ] [mu] hello", "[OK   ] [cp] done"} {
		if !strings.Contains(got, want) {
			t.Errorf("framework.log missing %q in:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, "[20") { // leading ISO timestamp
		t.Errorf("expected leading [timestamp], got:\n%s", got)
	}
}

func TestStep(t *testing.T) {
	logf := filepath.Join(t.TempDir(), "framework.log")
	t.Setenv("MU_LOG_FILE", logf)
	ResetLoggerForTest()

	out, err := Step("cp", "push", func() (int, error) { return 42, nil })
	if err != nil || out != 42 {
		t.Fatalf("Step success: out=%d err=%v", out, err)
	}
	if _, err := Step("cp", "pull", func() (int, error) { return 0, errors.New("boom") }); err == nil {
		t.Fatal("Step must propagate the error")
	}

	got := readFile(t, logf)
	for _, want := range []string{
		"[INFO ] [cp] push",       // start
		"[OK   ] [cp] push (",     // success end carries a duration
		"[INFO ] [cp] pull",       // start
		"[ERROR] [cp] pull: boom", // failure end
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
