package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Checkup throttle state for the shell-init background run. Disposable (a lost
// stamp just re-runs a day early), so it lives under ~/.cache — unlike events.log,
// which is history and moved to the state dir. The shell snippet recomputes these
// paths with builtins; keep them in lockstep with shellinit's doctorCheckup.

// CheckupEvery is the minimum gap between shell-init-triggered doctor runs.
const CheckupEvery = 24 * time.Hour

func checkupCacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "mayhl_utils")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "mayhl_utils")
}

// StampPath holds the epoch of the last checkup (one line, digits).
func StampPath() string { return filepath.Join(checkupCacheDir(), "doctor.stamp") }

// NoticePath is the WARN/FAIL summary the next shell start prints.
func NoticePath() string { return filepath.Join(checkupCacheDir(), "doctor.notice") }

// StampFresh reports whether a checkup ran within CheckupEvery of now — the mu-side
// re-check that collapses racing shells (each saw a stale stamp and backgrounded a run).
func StampFresh(now time.Time) bool {
	b, err := os.ReadFile(StampPath())
	if err != nil {
		return false
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return false
	}
	return now.Sub(time.Unix(sec, 0)) < CheckupEvery
}

// WriteStamp records now as the last checkup. Written BEFORE the checks run, so a
// slow run doesn't let a second shell double-fire.
func WriteStamp(now time.Time) error {
	p := StampPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(strconv.FormatInt(now.Unix(), 10)+"\n"), 0o644)
}

// NoticeText is the one-liner for warn/fail counts; "" when healthy.
func NoticeText(warn, fail int) string {
	var parts []string
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("%d fail", fail))
	}
	if warn > 0 {
		parts = append(parts, fmt.Sprintf("%d warn", warn))
	}
	if len(parts) == 0 {
		return ""
	}
	return "mu doctor: " + strings.Join(parts, ", ") + " (mu doctor -v for detail)\n"
}

// UpdateNotice writes doctor.notice per the tallies, or clears it when healthy —
// so the nag self-heals the first run after the problem is fixed.
func UpdateNotice(warn, fail int) error {
	p := NoticePath()
	txt := NoticeText(warn, fail)
	if txt == "" {
		err := os.Remove(p)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(txt), 0o644)
}
