package job

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hooks"
)

// watchWorld builds the two-tier hooks layout (project checkout on "home", run
// dir on "work") and points MU_RUN_DIR at the run dir — the post-prep state
// Watch sees. The progress hook echoes the pct file's contents so tests drive
// the sim forward (or stall it) by rewriting one file.
func watchWorld(t *testing.T) (runDir, pctFile, hook string) {
	t.Helper()
	// first-exec of a fresh script can take seconds under the laptop AV — don't
	// let the contract timeout flake the suite
	old := hooks.Timeout
	hooks.Timeout = 60 * time.Second
	t.Cleanup(func() { hooks.Timeout = old })
	base := t.TempDir()
	home, work := filepath.Join(base, "home"), filepath.Join(base, "work")
	sim := "proj/simulations/funwave"
	runDir = filepath.Join(work, sim, "case_a_250")
	hooksDir := filepath.Join(home, "proj/scripts/funwave/hooks")
	for _, p := range []string{
		filepath.Join(home, sim, "case_a"), runDir, hooksDir,
		filepath.Join(home, "proj/.git"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pctFile = filepath.Join(runDir, "pct")
	if err := os.WriteFile(pctFile, []byte("10"), 0o644); err != nil {
		t.Fatal(err)
	}
	hook = filepath.Join(hooksDir, "progress")
	script := "#!/bin/sh\nprintf '{\"pct\": %s}' \"$(cat pct)\"\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("WORKDIR", work)
	t.Setenv("ARCHIVE_HOME", "/arch")
	t.Setenv("MU_CONFIG_FILE", filepath.Join(base, "nonexistent.toml"))
	t.Setenv("MU_ROOT", "")
	t.Setenv("MU_RUN_DIR", runDir)
	t.Setenv("MU_JOBID", "250.server")
	config.ResetForTest()
	return runDir, pctFile, hook
}

// readLines parses .mu/progress into tick lines ("" file → nil).
func readLines(t *testing.T, runDir string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(runDir, ".mu", "progress"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil // opened but nothing flushed yet
	}
	var out []map[string]any
	for _, ln := range strings.Split(trimmed, "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("torn line %q: %v", ln, err)
		}
		out = append(out, m)
	}
	return out
}

// waitFor polls until cond sees the stream state it wants — ticks are timed,
// so assertions must be patient rather than counted.
func waitFor(t *testing.T, runDir string, what string, cond func([]map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if cond(readLines(t, runDir)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; stream: %v", what, readLines(t, runDir))
}

func events(lines []map[string]any) []string {
	var out []string
	for _, l := range lines {
		if e, ok := l["event"].(string); ok {
			out = append(out, e)
		}
	}
	return out
}

func TestWatchNoHookIsNoOp(t *testing.T) {
	runDir, _, hook := watchWorld(t)
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	ok, err := Watch(context.Background(), time.Millisecond)
	if err != nil || ok {
		t.Fatalf("hook-less watch: ok=%v err=%v, want false nil", ok, err)
	}
	if _, err := os.Stat(filepath.Join(runDir, ".mu")); !os.IsNotExist(err) {
		t.Fatal(".mu created despite no hook")
	}
}

func TestWatchSnapshotsAndStall(t *testing.T) {
	runDir, pctFile, _ := watchWorld(t)
	old := StallTicks
	StallTicks = 3
	t.Cleanup(func() { StallTicks = old })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Watch(ctx, 20*time.Millisecond)
		done <- err
	}()

	// constant pct → a single stall event once the run length hits StallTicks
	waitFor(t, runDir, "stall event", func(ls []map[string]any) bool {
		return len(events(ls)) == 1 && events(ls)[0] == "stall"
	})
	// forward motion → resumed, exactly once
	if err := os.WriteFile(pctFile, []byte("42"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, runDir, "resumed event", func(ls []map[string]any) bool {
		ev := events(ls)
		return len(ev) == 2 && ev[1] == "resumed"
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("watch: %v", err)
	}

	lines := readLines(t, runDir)
	sawOld, sawNew := false, false
	for _, l := range lines {
		if l["event"] != nil {
			continue
		}
		if l["t"] == nil {
			t.Fatalf("snapshot missing timestamp: %v", l)
		}
		data, _ := l["data"].(map[string]any)
		switch data["pct"] {
		case 10.0:
			sawOld = true
		case 42.0:
			sawNew = true
		default:
			t.Fatalf("unexpected snapshot data: %v", l)
		}
	}
	if !sawOld || !sawNew {
		t.Fatalf("stream missing a phase (old=%v new=%v): %v", sawOld, sawNew, lines)
	}
}
