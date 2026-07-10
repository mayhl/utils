package doctor

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestStampFreshness(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	now := time.Unix(1_700_000_000, 0)

	if StampFresh(now) {
		t.Error("no stamp: want stale")
	}
	if err := WriteStamp(now); err != nil {
		t.Fatal(err)
	}
	if !StampFresh(now.Add(CheckupEvery - time.Minute)) {
		t.Error("within the interval: want fresh")
	}
	if StampFresh(now.Add(CheckupEvery + time.Minute)) {
		t.Error("past the interval: want stale")
	}

	// Corrupt stamp reads as stale, never as an error.
	if err := os.WriteFile(StampPath(), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if StampFresh(now) {
		t.Error("corrupt stamp: want stale")
	}
}

func TestUpdateNotice(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	// Healthy with no notice on disk: a clean no-op.
	if err := UpdateNotice(0, 0); err != nil {
		t.Fatalf("healthy, no notice: %v", err)
	}

	if err := UpdateNotice(2, 1); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(NoticePath())
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, w := range []string{"1 fail", "2 warn", "mu doctor -v"} {
		if !strings.Contains(got, w) {
			t.Errorf("notice missing %q: %q", w, got)
		}
	}

	// Warn-only notice omits the fail clause.
	if err := UpdateNotice(1, 0); err != nil {
		t.Fatal(err)
	}
	if b, _ = os.ReadFile(NoticePath()); strings.Contains(string(b), "fail") {
		t.Errorf("warn-only notice mentions fail: %q", b)
	}

	// Healthy run clears the nag.
	if err := UpdateNotice(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(NoticePath()); !os.IsNotExist(err) {
		t.Error("healthy: notice should be removed")
	}
}
