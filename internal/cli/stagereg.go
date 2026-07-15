package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// The staged-script registry.
//
// `mu job sub` PUSHES a local script to the cluster (stageDir) and submits the staged copy.
// Unlike a tunnel, a batch job has no close event and mu exits the moment it's submitted, so
// nothing would ever remove that copy — it would accumulate one file per push forever. This
// registry is the bookkeeping that lets a later sweep reap it: one record per push, dropped
// once the job it fed has left the queue.
//
// Bookkeeping, not state: losing a record only orphans one small script on the shared FS (a
// leak, not a stranded job or a held port), so — unlike the tunnel registry — a write failure
// is a warning, not a submit-blocking error.

// stagedRec is one pushed job-sub script awaiting cleanup. ID is the staged filename stem (the
// mu handle minted at push). Node is the -N value the sub used ("" = the local login cluster),
// the routing a sweep needs to reach the file and ask the scheduler whether the job is done.
type stagedRec struct {
	ID      string    `json:"id"`     // staged filename stem, e.g. "3f9a" (file mu-side: <id>.sh)
	Node    string    `json:"node"`   // -N value; "" = local cluster
	System  string    `json:"system"` // resolved cluster label (for messages)
	Job     string    `json:"job"`    // scheduler job id the staged script was submitted as
	Started time.Time `json:"started"`
}

// stagedRegDir is where the staged-script records live — STATE_HOME, alongside the tunnel
// registry, since both track things that outlive the mu that made them.
func stagedRegDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "mayhl_utils", "staged")
}

func stagedRegPath(id string) string {
	safe := strings.NewReplacer("/", "_", " ", "_").Replace(id)
	return filepath.Join(stagedRegDir(), safe+".json")
}

// saveStaged records a pushed script. Best-effort by contract — the caller warns rather than
// fails, since a lost record costs at most one orphaned file, never a lost job.
func saveStaged(r stagedRec) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	p := stagedRegPath(r.ID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// loadStaged reads the registry, newest first; an unreadable entry is skipped, not fatal.
func loadStaged() []stagedRec {
	ents, err := os.ReadDir(stagedRegDir())
	if err != nil {
		return nil
	}
	var out []stagedRec
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(stagedRegDir(), e.Name()))
		if err != nil {
			continue
		}
		var r stagedRec
		if json.Unmarshal(b, &r) == nil && r.ID != "" {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

func forgetStaged(r stagedRec) { _ = os.Remove(stagedRegPath(r.ID)) }

// sweepStagedOn reaps the pushed job-sub scripts on ONE cluster whose job has left the queue,
// reusing a connection the caller already holds. Liveness comes from the queue LISTING, not a
// per-job `qstat -f`: a detail query exits non-zero the moment any id is already gone, which
// is indistinguishable from an ssh failure, whereas a job simply absent from a successful
// listing is definitively done. So the rule is safe in both directions — a job still in the
// listing keeps its script (it may yet be read at run time); a failed listing reaps nothing
// (an unanswered query is not proof the job ended). Returns the count removed; verbose reports
// each removal (the `mu job clean` path); the opportunistic sweep runs it silent.
func sweepStagedOn(node, label string, snapshot func() ([]queue.Job, error), capture func(string) (string, error), verbose bool) int {
	var group []stagedRec
	for _, r := range loadStaged() {
		if r.Node == node {
			group = append(group, r)
		}
	}
	if len(group) == 0 {
		return 0
	}
	jobs, err := snapshot()
	if err != nil {
		return 0 // can't see the queue right now — leave every record for a later sweep
	}
	live := map[string]bool{}
	for _, j := range jobs {
		live[j.ID] = true
		live[j.ShortID] = true
	}
	// Collect every record whose job has left the queue, then remove them all in ONE `rm -f`
	// rather than a connection per file.
	var dead []stagedRec
	var files []string
	for _, r := range group {
		// The stored id can carry a different host suffix than the listing echoes, so match
		// on the suffix-free segment too (jobShort) — same drift the tunnel sweep guards.
		if live[r.Job] || live[jobShort(r.Job)] {
			continue // still queued or running — the script must survive until the job ends
		}
		dead = append(dead, r)
		files = append(files, fmt.Sprintf(`"$HOME/.local/state/mayhl_utils/jobs/%s.sh"`, r.ID))
	}
	if len(dead) == 0 {
		return 0
	}
	// rm -f never fails on a missing file, so an error here is the connection, not the files —
	// forget nothing (a lost record beats a phantom "reaped" for a file still on disk).
	if _, err := capture("rm -f " + strings.Join(files, " ")); err != nil {
		if verbose {
			render.Warn(fmt.Sprintf("%s: couldn't remove staged scripts: %v", label, err))
		}
		return 0
	}
	for _, r := range dead {
		forgetStaged(r)
		if verbose {
			render.OK(fmt.Sprintf("reaped staged script %s (job %s ended)", r.ID, r.Job))
		}
	}
	return len(dead)
}
