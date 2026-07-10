package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hooks"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/modules"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/shell"
)

// hooksBudget caps the whole batch on top of the per-hook timeout — a queue
// listing must never wait on a pile of slow hooks.
const hooksBudget = 10 * time.Second

// jobHooksCmd is `mu job hooks`: the read-time model-hooks runner. It executes
// ON a login node (the local mstat/minfo side invokes it over ssh, concurrent
// with the queue snapshot) and is deliberately self-contained: its own queue
// list + detail fetch, run dir by the prep rule (<submitdir>_<shortid>, absent
// = unprepped → skip), contract discovery, per-hook timeout, JSON-lines out —
// a dumb protocol so mild local↔remote version skew is harmless.
func jobHooksCmd() *cobra.Command {
	var jobsCSV string
	var full bool
	c := &cobra.Command{
		Use:   "hooks",
		Short: "Run model hooks for your running jobs, emitting JSON lines.",
		Long: "Run each job's model hooks per the contract (CWD = run dir, MU_JOBID env,\n" +
			"flat-JSON stdout) and emit one line per result: {\"job\",\"hook\",\"exit\",\"data\"}.\n" +
			"List mode runs only the progress hook; --full (single-job inspect) runs them\n" +
			"all. Jobs without a prepped run dir or without hooks are skipped silently —\n" +
			"missing model data is a normal state, not an error.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runJobHooks(jobsCSV, full)
		},
	}
	c.Flags().StringVar(&jobsCSV, "jobs", "", "comma-separated job ids (default: all your running jobs)")
	c.Flags().BoolVar(&full, "full", false, "run every hook, not just progress")
	return c
}

// hookLine is one emitted result — the job id wrapped around the contract Result.
type hookLine struct {
	Job string `json:"job"`
	hooks.Result
}

func runJobHooks(jobsCSV string, full bool) error {
	self, scheduler := currentCluster()
	if self == "" {
		return usageErr("not on an HPC cluster — `mu job hooks` runs on a login node")
	}
	a := queue.For(scheduler)
	if a == nil {
		return errNoScheduler(self)
	}

	ids := splitCSV(jobsCSV)
	if len(ids) == 0 {
		cmd, parse := fetchSpec(scheduler, userSel{})
		out, err := hpc.LocalExec(cmd)
		if err != nil {
			return runErr("%s: local queue fetch failed: %v", self, err)
		}
		for _, j := range parse(out) {
			if j.State == queue.Running {
				ids = append(ids, j.ShortID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}

	out, err := hpc.LocalExec(a.DetailCmd(ids))
	if err != nil {
		return runErr("%s: detail fetch failed: %v", self, err)
	}
	enc := json.NewEncoder(os.Stdout)
	deadline := time.Now().Add(hooksBudget)
	for _, d := range queue.ParseDetails(scheduler, out) {
		if d.WorkDir == "" || d.ShortID == "" {
			continue
		}
		runDir := d.WorkDir + "_" + d.ShortID
		if info, err := os.Stat(runDir); err != nil || !info.IsDir() {
			continue // unprepped — no run dir, nothing to probe
		}
		names := []string{"progress"}
		if full {
			names = hooks.List(runDir)
		}
		for _, name := range names {
			if time.Now().After(deadline) {
				return nil // budget spent — emit what we have, stay silent
			}
			h, ok := hooks.Find(runDir, name)
			if !ok {
				continue
			}
			line := hookLine{Job: d.ShortID, Result: hooks.Exec(h, runDir, d.ShortID)}
			if err := enc.Encode(line); err != nil {
				return runErr("%v", err)
			}
		}
	}
	return nil
}

// splitCSV splits a comma list, dropping empties ("" → nil).
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseHookLines reads `mu job hooks` JSON-lines output into per-job hook
// results — the LOCAL side of the protocol. Junk lines (login noise that
// escaped the stderr filter, version-skewed fields) are dropped silently.
func ParseHookLines(raw string) map[string][]hooks.Result {
	out := map[string][]hooks.Result{}
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || ln[0] != '{' {
			continue
		}
		var l hookLine
		if err := json.Unmarshal([]byte(ln), &l); err != nil || l.Job == "" {
			continue
		}
		out[l.Job] = append(out[l.Job], l.Result)
	}
	return out
}

// hookProgress projects the standard progress key out of a job's results:
// `{"pct": 38, …}` → "38%". Absent/malformed → "" (the column renders empty).
func hookProgress(results []hooks.Result) string {
	for _, r := range results {
		if r.Hook != "progress" || r.Data == nil {
			continue
		}
		if pct, ok := r.Data["pct"].(float64); ok {
			return fmt.Sprintf("%.0f%%", pct)
		}
	}
	return ""
}

// hookWait caps how long a rendered listing waits on the concurrent hooks fetch
// AFTER its snapshot is in hand — late data drops the column, never delays the
// table.
const hookWait = 3 * time.Second

// fetchHookProgress launches the read-time hooks fetch concurrent with a queue
// snapshot: call before the fetch, hand the channel to applyHookProgress /
// awaitHookProgress after. Returns nil (→ no model column) when the project
// module or the [project] job_hooks switch is off. Every failure path degrades
// to no data — a missing remote mu, a dead host, an expired-ticket ssh refusal
// all just lose the column for this listing.
func fetchHookProgress(node string, local bool) <-chan map[string]string {
	if !modules.Enabled("project") || !config.JobHooks() {
		return nil
	}
	ch := make(chan map[string]string, 1)
	go func() {
		var out string
		var err error
		if local {
			out, err = hpc.LocalExec("mu job hooks")
		} else {
			target, terr := hpc.Resolve(node)
			if terr != nil {
				ch <- nil
				return
			}
			out, err = hpc.RemoteExecTimeout(target, "mu job hooks", collateTimeout)
		}
		if err != nil {
			ch <- nil
			return
		}
		m := map[string]string{}
		for id, rs := range ParseHookLines(out) {
			if p := hookProgress(rs); p != "" {
				m[id] = p
			}
		}
		ch <- m
	}()
	return ch
}

// awaitHookProgress collects the fetched progress, waiting at most hookWait
// past the snapshot (nil channel or late data → nil).
func awaitHookProgress(ch <-chan map[string]string) map[string]string {
	if ch == nil {
		return nil
	}
	select {
	case m := <-ch:
		return m
	case <-time.After(hookWait):
		return nil
	}
}

// applyHookProgress folds the fetched progress into single-cluster rows by job id.
func applyHookProgress(rows []render.JobRow, ch <-chan map[string]string) {
	m := awaitHookProgress(ch)
	if m == nil {
		return
	}
	for i := range rows {
		rows[i].Prog = m[rows[i].ID]
	}
}

// applyFleetHookProgress folds collated progress into merged rows via the
// "label/id" keys — each row's Cluster carries its target label.
func applyFleetHookProgress(rows []render.JobRow, prog map[string]string) {
	if len(prog) == 0 {
		return
	}
	for i := range rows {
		rows[i].Prog = prog[rows[i].Cluster+"/"+rows[i].ID]
	}
}

// fetchHookModel launches the --full hooks run for an inspect card, concurrent
// with the detail fetch, through the same capture seam (local or remote). Nil
// channel = module/switch off; failures degrade to no Model section.
func fetchHookModel(capture func(string) (string, error), ids []string) <-chan map[string][][2]string {
	if !modules.Enabled("project") || !config.JobHooks() {
		return nil
	}
	ch := make(chan map[string][][2]string, 1)
	go func() {
		out, err := capture("mu job hooks --full --jobs " + shell.Quote(strings.Join(ids, ",")))
		if err != nil {
			ch <- nil
			return
		}
		m := map[string][][2]string{}
		for id, rs := range ParseHookLines(out) {
			m[id] = orderedModel(rs)
		}
		ch <- m
	}()
	return ch
}

// awaitHookModel collects the --full results, bounded like applyHookProgress.
func awaitHookModel(ch <-chan map[string][][2]string) map[string][][2]string {
	if ch == nil {
		return nil
	}
	select {
	case m := <-ch:
		return m
	case <-time.After(hookWait):
		return nil
	}
}

// orderedModel flattens hook results into the card's key/value pairs: the
// standard progress keys lead in a fixed order, remaining keys follow sorted —
// stable cards from unordered JSON maps.
func orderedModel(results []hooks.Result) [][2]string {
	merged := map[string]string{}
	for _, r := range results {
		for k, v := range r.Data {
			merged[k] = fmt.Sprint(v)
		}
	}
	var out [][2]string
	for _, k := range []string{"pct", "sim_t", "eta", "walltime_est"} {
		if v, ok := merged[k]; ok {
			out = append(out, [2]string{k, v})
			delete(merged, k)
		}
	}
	rest := make([]string, 0, len(merged))
	for k := range merged {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		out = append(out, [2]string{k, merged[k]})
	}
	return out
}
