package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// showQueuesCmd is the site command mu runs to enumerate a cluster's batch queues
// (limits + live counts). It emits the same wide format on PBS and SLURM, so one parser
// covers both; it is broken/absent on some systems, which the fetch degrades gracefully.
const showQueuesCmd = "show_queues"

// hpcQueuesCmd is `mu hpc queues`: list a cluster's batch queues (type, limits, live
// counts, up/stopped/disabled state) as a house table. Sibling of `mu hpc queue` (which
// lists jobs). One `show_queues` parser covers PBS and SLURM; target like queue: --node
// fetches one cluster over remote-exec, --local runs it on the current cluster, else
// stdin is parsed.
func hpcQueuesCmd() *cobra.Command {
	var node string
	var local, jsonOut, all, interactive bool
	c := &cobra.Command{
		Use:   "queues",
		Short: "Show a cluster's batch queues (show_queues) as a house table.",
		Long: "List a cluster's batch queues — walltime/job/core limits, live run/pend counts,\n" +
			"and each queue's up / stopped / disabled state — as one house table. `show_queues`\n" +
			"emits the same wide format on PBS and SLURM, so one parser covers both.\n\n" +
			"By default only submittable (Exe) queues that are up are shown, with a compact\n" +
			"column set. Since they're all Exe and all up, the Type and State columns are\n" +
			"dropped as noise; -a/--all brings them back and adds the routing/admin and\n" +
			"down queues too. A submittable queue that's stopped/disabled is warned about.\n\n" +
			"Target, like `mu hpc queue`: --node fetches one cluster over remote-exec, --local\n" +
			"runs it on the current cluster (no ssh), and with neither a listing piped on stdin\n" +
			"is parsed — the test/pipe-your-own seam:\n" +
			"    mu hpc queues --node hpc1\n" +
			"    hpc1 show_queues | mu hpc queues",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if interactive && !render.Interactive() {
				return fmt.Errorf("mu hpc queues -i needs a terminal (stdin is not a tty)")
			}
			var label string
			var qs []queue.QueueInfo
			switch {
			case node != "":
				label, qs = fetchQueues(node)
			case local:
				label, qs = fetchQueuesLocal()
			case !term.IsTerminal(os.Stdin.Fd()):
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				label, qs = "queues", queue.ParseShowQueues(string(data))
				if len(qs) == 0 {
					render.Warn("no queues parsed — is this `show_queues` output?")
					return nil
				}
			default:
				// Bare `mu hpc queues`, no pipe → resolve by location: on a login node run
				// locally; off HPC (no local scheduler) steer to --node or a pipe.
				if self, _ := currentCluster(); self != "" {
					label, qs = fetchQueuesLocal()
				} else {
					render.Warn("not on an HPC cluster — use `mu hpc queues --node <n>` or pipe `show_queues`")
					os.Exit(2)
				}
			}
			raw := len(qs)
			if !all {
				// Default view: submittable (Exe) queues, then keep only the up ones —
				// the up filter is applied LAST (after Exe and any other filters). After
				// this they should all be up, so QueuesTable drops the Type/State columns;
				// a nonzero down count means a submittable queue is stopped/disabled, worth
				// a heads-up.
				qs = execQueues(qs)
				up, down := upQueues(qs)
				if down > 0 && len(up) > 0 {
					render.Warn(fmt.Sprintf("%d submittable queue(s) not up (stopped/disabled) — -a to show", down))
				}
				qs = up
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(qs)
			}
			if len(qs) == 0 {
				// raw>0 means everything was filtered out (routing/admin or all down); a
				// broken fetch (raw==0) already warned, so stay quiet there.
				if raw > 0 && !all {
					render.Warn(fmt.Sprintf("no up submittable queues on %s — -a to show all %d", label, raw))
				}
				return nil
			}
			if interactive {
				return queuesInteractive(label, qs, all)
			}
			render.QueuesTable(label, toQueueRows(label, qs), all) // Type/State columns only under -a
			return nil
		},
	}
	c.Flags().StringVarP(&node, "node", "N", "", "fetch queues from this node (else read stdin)")
	c.Flags().BoolVarP(&local, "local", "l", false, "run show_queues on the current cluster, locally (no ssh)")
	c.Flags().BoolVarP(&all, "all", "a", false, "include routing/admin (non-Exe) queues (default: only submittable Exe queues)")
	c.Flags().BoolVarP(&interactive, "interactive", "i", false, "browse queues in a live-filterable picker (type to narrow, `i` to inspect)")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit queues as JSON (complete, untruncated) instead of a table")
	c.MarkFlagsMutuallyExclusive("node", "local")
	c.MarkFlagsMutuallyExclusive("json", "interactive")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

// fetchQueues runs show_queues on node over remote-exec and parses it (same format on
// PBS and SLURM — it's a site wrapper). show_queues is broken/absent on some systems, so
// a run failure degrades to a warning and an empty result, never a crash. Mirrors fetchJobs.
func fetchQueues(node string) (string, []queue.QueueInfo) {
	target, err := hpc.Resolve(node)
	if err != nil {
		render.Err(err.Error())
		os.Exit(2)
	}
	hpc.EnsureTicket()
	out, err := hpc.RemoteExec(target, showQueuesCmd)
	if err != nil {
		render.Warn(fmt.Sprintf("%s: show_queues failed (broken or unsupported on this system?): %v", node, err))
		return node, nil
	}
	return node, queue.ParseShowQueues(out)
}

// fetchQueuesLocal runs show_queues on the current cluster (no ssh) — the on-HPC path.
// bash -lc so the site profile puts show_queues on PATH; same graceful degradation as
// fetchQueues when show_queues is broken here.
func fetchQueuesLocal() (string, []queue.QueueInfo) {
	self, _ := currentCluster()
	if self == "" {
		render.Warn("not on an HPC cluster — use `mu hpc queues --node <n>` or pipe `show_queues`")
		os.Exit(2)
	}
	out, err := exec.Command("bash", "-lc", showQueuesCmd).Output()
	if err != nil {
		render.Warn(fmt.Sprintf("%s: local show_queues failed (broken or unsupported here?): %v", self, err))
		return self, nil
	}
	return self, queue.ParseShowQueues(string(out))
}

// execQueues keeps only submittable (Exe) queues — part of the default `mu hpc queues`
// view. Non-Exe queues are routing/admin queues a user never targets with `mu job sub`,
// and they're what the future -q completion cache filters on too. Matches the `Exe` Type
// code case-insensitively by prefix, tolerating a longer spelling on some systems.
func execQueues(qs []queue.QueueInfo) []queue.QueueInfo {
	out := make([]queue.QueueInfo, 0, len(qs))
	for _, q := range qs {
		if strings.HasPrefix(strings.ToLower(q.Type), "exe") {
			out = append(out, q)
		}
	}
	return out
}

// upQueues splits queues into the ones that are up and a count of the rest — the last
// filter in the default view. "Up" is defined as NOT known-down: a queue is dropped only
// when it is explicitly stopped/disabled (E=N or R=N); blank/"-" flags (systems that don't
// report state) are kept, since we can't confirm they're down. After the Exe filter the
// survivors should all be up, so a nonzero down count is abnormal and the caller warns.
func upQueues(qs []queue.QueueInfo) (up []queue.QueueInfo, down int) {
	for _, q := range qs {
		if q.Enabled == "N" || q.Running == "N" {
			down++
			continue
		}
		up = append(up, q)
	}
	return up, down
}

// toQueueRows maps parsed QueueInfo to render's plain QueueRow (keeping render domain-
// free). label is the cluster, used to resolve the config class/cores overrides: Class is
// the config override or the name heuristic, and MaxNodes = ceil(MaxCores / cores-per-node)
// with a per-queue cores override falling back to the cluster default.
func toQueueRows(label string, qs []queue.QueueInfo) []render.QueueRow {
	rows := make([]render.QueueRow, len(qs))
	for i, q := range qs {
		rows[i] = render.QueueRow{
			Name: q.Name, Class: queueClass(label, q.Name), Type: q.Type,
			Walltime: q.MaxWalltime, MaxJobs: q.MaxJobs, MaxCores: q.MaxCores,
			MaxNodes: queueMaxNodes(label, q.Name, q.MaxCores),
			Run:      q.JobsRun, Pend: q.JobsPend, Enabled: q.Enabled, Running: q.Running,
		}
	}
	return rows
}

// queueClass resolves a queue's node class: the config queue→class override if set, else
// the generic name heuristic.
func queueClass(label, name string) string {
	if c := config.QueueClassOverride(label, name); c != "" {
		return c
	}
	return queue.ClassifyQueue(name)
}

// queueMaxNodes returns the max nodes a job can span in a queue = ceil(MaxCores /
// cores-per-node), or "" when cores-per-node is unconfigured (0) or MaxCores isn't a
// positive integer. cores-per-node is the per-queue override if set, else the cluster
// default — GPU/specialty queues can list a MaxCores that doesn't match the node's CPU
// core count, so their divisor is overridden in config.toml.
func queueMaxNodes(label, name, maxCores string) string {
	cpn := config.QueueCoresOverride(label, name)
	if cpn == 0 {
		cpn = config.CoresPerNodeFor(label)
	}
	return maxNodesFrom(maxCores, cpn)
}

// maxNodesFrom is the pure core of queueMaxNodes: ceil(maxCores / coresPerNode) as a
// string, or "" when coresPerNode is unset (≤0) or maxCores isn't a positive integer.
func maxNodesFrom(maxCores string, coresPerNode int) string {
	if coresPerNode <= 0 {
		return ""
	}
	mc, err := strconv.Atoi(strings.TrimSpace(maxCores))
	if err != nil || mc <= 0 {
		return ""
	}
	return strconv.Itoa((mc + coresPerNode - 1) / coresPerNode) // ceil
}

// queuesInteractive is `mu hpc queues -i`: a live-filterable, scrollable viewer over the
// queue list (read-only — queues aren't acted on), so a list noisy with project-specific
// queues can be narrowed by typing. `i` inspects a queue's full limits. Snapshot-based —
// the rows are fetched once (queues rarely change), so the refresh tick doesn't re-ssh.
func queuesInteractive(label string, qs []queue.QueueInfo, all bool) error {
	if !render.Interactive() {
		return fmt.Errorf("mu hpc queues -i needs a terminal (stdin is not a tty)")
	}
	byName := make(map[string]queue.QueueInfo, len(qs))
	for _, q := range qs {
		byName[q.Name] = q
	}
	qrows := toQueueRows(label, qs)
	cols := render.QueueColumns(qrows, all) // shed low-priority columns to fit, like the table
	display := make([]string, len(cols))
	for i, h := range cols {
		display[i] = strings.ToUpper(h) // picker headers are uppercase (like mstat -i)
	}
	selRows := make([]render.SelectRow, len(qrows))
	for i, r := range qrows {
		cells := make([]string, len(cols))
		hues := make([]string, len(cols))
		for j, h := range cols {
			cells[j], hues[j] = queuePickerCell(h, r)
		}
		selRows[i] = render.SelectRow{ID: r.Name, Cells: cells, Hues: hues}
	}
	return render.Viewer(render.SelectSpec{
		Title:      label + " queues",
		Columns:    display,
		Interval:   time.Hour, // static snapshot — queues rarely change; don't refetch on the tick
		Fetch:      func() []render.SelectRow { return selRows },
		Detail:     queueDetailCard(label, byName),
		FacetCol:   2, // Class is always the 2nd column — `f` cycles all → CPU → GPU → … → all
		FacetLabel: "class",
	})
}

// queuePickerCell returns the plain cell text + house hue for a queues-picker column, so
// the picker colors cells via SelectRow.Hues (keeping them plain for the reverse-video
// cursor row) exactly as the static table colors its columns.
func queuePickerCell(header string, r render.QueueRow) (string, string) {
	switch header {
	case "Queue":
		return r.Name, render.HueGroup
	case "Class":
		return orDash(r.Class), render.HueUser // magenta
	case "Type":
		return orDash(r.Type), render.HueDim
	case "Walltime":
		return orDash(r.Walltime), render.HueName
	case "MaxJobs":
		return orDash(r.MaxJobs), ""
	case "MaxCores":
		return orDash(r.MaxCores), ""
	case "MaxNodes":
		return orDash(r.MaxNodes), ""
	case "Run":
		return orDash(r.Run), ""
	case "Pend":
		return orDash(r.Pend), ""
	case "Load":
		return render.QueueLoad(r.Run, r.Pend)
	case "State":
		return render.QueueState(r.Enabled, r.Running)
	default:
		return "", ""
	}
}

// queueDetailCard returns the `i`-inspect renderer for the queue picker: a house KVCard
// with the queue's derived class/load/state plus the full limits and per-state core figures
// the table omits, looked up by name from the fetched snapshot.
func queueDetailCard(label string, byName map[string]queue.QueueInfo) func(string) string {
	return func(name string) string {
		q, ok := byName[name]
		if !ok {
			return ""
		}
		loadLabel, loadHue := render.QueueLoad(q.JobsRun, q.JobsPend)
		stateLabel, stateHue := render.QueueState(q.Enabled, q.Running)
		title := render.Bold("Queue "+q.Name, render.HueGroup) + "   ·   " +
			render.Fg(queueClass(label, q.Name), render.HueUser)
		return render.KVCard(title, []render.KVField{
			{Label: "State", Value: stateLabel, Hue: stateHue},
			{Label: "Load", Value: loadLabel, Hue: loadHue},
			{Label: "Type", Value: orDash(q.Type), Hue: render.HueDim},
			{Label: "Walltime", Value: orDash(q.MaxWalltime), Hue: render.HueName},
			{Label: "Max jobs", Value: orDash(q.MaxJobs)},
			{Label: "Cores/job", Value: coresRange(q.MinCores, q.MaxCores)},
			{Label: "Max nodes", Value: orDash(queueMaxNodes(label, q.Name, q.MaxCores))},
			{Label: "Running", Value: countCores(q.JobsRun, q.CoresRun)},
			{Label: "Pending", Value: countCores(q.JobsPend, q.CoresPend)},
		})
	}
}

// coresRange formats a queue's per-job core limits as "min – max" (or the one that's set),
// or "--" when neither is.
func coresRange(minC, maxC string) string {
	lo, hi := strings.TrimSpace(minC), strings.TrimSpace(maxC)
	switch {
	case lo == "" && hi == "":
		return "--"
	case lo == "":
		return hi
	case hi == "":
		return lo
	default:
		return lo + " – " + hi
	}
}

// countCores formats a "N jobs / M cores" line for the running/pending detail rows.
func countCores(jobs, cores string) string {
	return orDash(jobs) + " jobs / " + orDash(cores) + " cores"
}

// orDash normalizes an empty / "--" queue field to a single "--" placeholder (the picker
// counterpart of render's internal dash).
func orDash(s string) string {
	if strings.TrimSpace(s) == "" || s == "--" {
		return "--"
	}
	return s
}
