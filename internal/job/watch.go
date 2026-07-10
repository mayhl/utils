package job

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mayhl/mayhl_utils/internal/hooks"
)

// watch.go is the sidecar invocation mode of the model-hooks contract: a tick
// loop the job preamble backgrounds (`mu job watch &`, trap-killed on exit),
// running INSIDE the allocation and appending one JSON line per tick to
// <rundir>/.mu/progress. The file is the whole product — last line = current
// state, the file = progress history and the tick stream the future
// notifications design consumes. Read-time (`mu job hooks`) stays independent;
// FUTURE: let it short-circuit to a fresh last line instead of re-exec'ing.

// StallTicks is how many consecutive unchanged-pct ticks flag a stall — at the
// default 60s interval, ~10 minutes without forward progress. Record-only: a
// stall/resumed event line joins the stream; consumers judge. A var so tests
// can tighten it.
var StallTicks = 10

// tickLine is one line of .mu/progress: a snapshot (Exit/Data/Err from the
// hook contract) or a stall marker (Event = "stall"/"resumed", Ticks = the
// unchanged run length). Failed ticks are recorded too — a hook that starts
// succeeding once output files appear is a normal history.
type tickLine struct {
	T     string         `json:"t"`
	Event string         `json:"event,omitempty"`
	Ticks int            `json:"ticks,omitempty"`
	Exit  int            `json:"exit,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
	Err   string         `json:"err,omitempty"`
}

// Watch ticks the progress hook every interval until ctx is done, appending
// snapshots to <rundir>/.mu/progress. The run dir comes from MU_RUN_DIR (prep
// already eval'd) with the jobContext derivation as fallback; erring only when
// not inside a job at all. No progress hook → ok=false, nil: the preamble line
// must be a graceful no-op on hook-less models.
func Watch(ctx context.Context, interval time.Duration) (ok bool, err error) {
	runDir := os.Getenv("MU_RUN_DIR")
	if runDir == "" {
		if runDir, err = RunDir(); err != nil {
			return false, err
		}
	}
	jobID := firstEnv("MU_JOBID", "SLURM_JOB_ID", "PBS_JOBID")
	hook, found := hooks.Find(runDir, "progress")
	if !found {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Join(runDir, ".mu"), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(runDir, ".mu", "progress"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	// one Write per line so a mid-tick kill can't interleave torn halves
	emit := func(l tickLine) error {
		l.T = time.Now().Format(time.RFC3339)
		b, err := json.Marshal(l)
		if err != nil {
			return err
		}
		_, err = f.Write(append(b, '\n'))
		return err
	}

	var lastPct float64
	havePct, stalled := false, false
	unchanged := 0
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		r := hooks.Exec(hook, runDir, jobID)
		if err := emit(tickLine{Exit: r.Exit, Data: r.Data, Err: r.Err}); err != nil {
			return true, err
		}
		// stall judgement rides the standard pct key only — wall-clock-derived
		// keys (eta) drift even when the sim is stuck; no pct → no judgement,
		// and a failing hook neither extends nor resets a run
		if pct, isNum := r.Data["pct"].(float64); isNum {
			switch {
			case havePct && pct == lastPct:
				unchanged++
				if unchanged >= StallTicks && !stalled {
					stalled = true
					if err := emit(tickLine{Event: "stall", Ticks: unchanged}); err != nil {
						return true, err
					}
				}
			default:
				if stalled {
					if err := emit(tickLine{Event: "resumed"}); err != nil {
						return true, err
					}
				}
				lastPct, havePct = pct, true
				unchanged, stalled = 0, false
			}
		}
		select {
		case <-ctx.Done():
			return true, nil
		case <-ticker.C:
		}
	}
}
