package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// tunnelLsCmd is `mu job tunnel ls`: the open tunnels, each enriched live from its scheduler.
// The registry says what mu STARTED; the scheduler says whether it's still alive — so a job
// that ended (walltime hit, cancelled elsewhere) is pruned here, on sight.
//
// Liveness is settled LOCALLY first, and that gate is the whole shape of this command: only a
// tunnel whose ssh master still answers is worth a scheduler query, so a registry of corpses
// lists instantly, offline, without a ticket. Listing must never be the thing that dials a
// cluster — least of all one you stopped using days ago.
func tunnelLsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ls",
		Short: "List the open background tunnels.",
		Long: "Show the tunnels `mu job tunnel` has opened in the background, each with its URL,\n" +
			"the job behind it and how much walltime is left. A job the scheduler no longer\n" +
			"knows (ended or cancelled) is dropped from the registry as it's noticed.\n\n" +
			"A tunnel whose ssh master is gone — a slept laptop, a dropped link — is shown as\n" +
			"detached: the forward is down, but the job may well still be running. Put it back\n" +
			"with `mu job tunnel reattach <id>`, which also reaps it if the job has ended.",
		Aliases: []string{"list"},
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			recs := loadTunnels()
			if len(recs) == 0 {
				render.Info("no open tunnels")
				return nil
			}
			live, detached := splitByMaster(recs)
			// One scheduler query per system (not per tunnel) enriches them all; a job the
			// scheduler no longer knows is pruned inside and left out of the map.
			states := tunnelStates(live)
			rows := make([]render.TunnelRow, 0, len(recs))
			for _, t := range recs {
				row := render.TunnelRow{ID: t.ID, Port: strconv.Itoa(t.LocalPort), System: t.System, Job: t.Job, Node: t.Host}
				switch {
				case detached[t.ID]:
					// The forward is provably down; the job's state is unknown and unasked.
					row.State = "detached"
				default:
					s, ok := states[t.ID]
					if !ok {
						continue // pruned — the job is gone
					}
					row.State, row.WallLeft = s.state, s.left
				}
				rows = append(rows, row)
			}
			if len(rows) == 0 {
				render.Info("no open tunnels")
				return nil
			}
			render.TunnelsTable(rows)
			if len(detached) > 0 {
				render.Detail("detached: the forward is gone — `mu job tunnel reattach <id>` reopens it, or reaps it if the job ended")
			}
			return nil
		},
	}
	return c
}

// splitByMaster sorts the registry by the one fact mu can establish without a cluster: whether
// each tunnel's ssh master still answers. Live ones are worth a scheduler query; the rest are
// detached — EXCEPT those whose walltime has provably run out, which are corpses and are reaped
// here, on sight and offline, the way a scheduler-confirmed-gone job is.
//
// A detached tunnel is deliberately NOT reaped: a dead master says the forward died, never that
// the job did, and dropping the record of a job still holding a compute node strands it.
func splitByMaster(recs []tunnelRec) (live []tunnelRec, detached map[string]bool) {
	detached = map[string]bool{}
	for _, t := range recs {
		switch {
		case masterAlive(t):
			live = append(live, t)
		case expired(t):
			forgetTunnel(t)
		default:
			detached[t.ID] = true
		}
	}
	return live, detached
}

// tunnelState is a tunnel's live scheduler-derived state for the `ls` table.
type tunnelState struct{ state, left string }

// tunnelStates enriches every tunnel with its live state, batching ONE scheduler query per
// system rather than one per tunnel. The returned map is keyed by tunnel id; a tunnel left
// OUT of it has been pruned — the scheduler has no record of its job, so the job ended and
// the record with it. A tunnel we couldn't judge (no scheduler configured, no ticket, an ssh
// hiccup) is KEPT with state "?": an unanswered query is not proof the job is gone.
func tunnelStates(recs []tunnelRec) map[string]tunnelState {
	out := make(map[string]tunnelState, len(recs))
	// Group by system, preserving first-seen order so one detailCmd covers all its jobs.
	var order []string
	bySystem := map[string][]tunnelRec{}
	for _, t := range recs {
		if _, seen := bySystem[t.System]; !seen {
			order = append(order, t.System)
		}
		bySystem[t.System] = append(bySystem[t.System], t)
	}
	ticketed := false
	for _, sys := range order {
		group := bySystem[sys]
		keepUnknown := func() {
			for _, t := range group {
				out[t.ID] = tunnelState{state: "?", left: t.Walltime}
			}
		}
		scheduler := config.SchedulerFor(sys)
		jobs := make([]string, len(group))
		for i, t := range group {
			jobs[i] = t.Job
		}
		cmd := detailCmd(scheduler, jobs)
		if cmd == "" {
			keepUnknown() // no scheduler configured — can't judge
			continue
		}
		// One ticket for the whole sweep, and only once there's a system to ask about.
		if !ticketed {
			if err := hpc.EnsureTicket(); err != nil {
				keepUnknown() // no ticket — can't ask, so don't prune
				continue
			}
			ticketed = true
		}
		raw, err := hpc.RemoteExec(group[0].Target, cmd)
		if err != nil {
			keepUnknown() // a transient ssh failure is not proof the jobs are gone
			continue
		}
		// Key results by BOTH full and short id: the id qsub returned ("1284575.sdb") can
		// carry a different host suffix than qstat echoes ("1284575.hpc1"), so match on the
		// suffix-free segment too. SLURM ids have no suffix, so both keys coincide.
		byJob := map[string]queue.JobDetail{}
		for _, d := range queue.ParseDetails(scheduler, raw) {
			byJob[d.ID] = d
			byJob[d.ShortID] = d
		}
		for _, t := range group {
			d, ok := byJob[t.Job]
			if !ok {
				d, ok = byJob[jobShort(t.Job)]
			}
			if !ok {
				forgetTunnel(t) // the scheduler has no record of it — the job is gone
				continue
			}
			out[t.ID] = tunnelState{state: d.State, left: wallLeft(d.ReqWall, d.Elapsed)}
		}
	}
	return out
}

// jobShort is the suffix-free leading segment of a scheduler job id (PBS "1284570.hpc1" →
// "1284570"; SLURM ids are unchanged), for matching a stored id against qstat's echoed form.
func jobShort(id string) string {
	if i := strings.IndexByte(id, '.'); i > 0 {
		return id[:i]
	}
	return id
}

// tunnelCloseCmd is `mu job tunnel close`: tear down a tunnel — drop the forward, kill the
// job, forget the record. --keep-job leaves the allocation running (you're only detaching
// the port); --all closes every open tunnel.
func tunnelCloseCmd() *cobra.Command {
	var all, keepJob, yes bool
	c := &cobra.Command{
		Use:   "close [job]",
		Short: "Close a background tunnel (and cancel its job).",
		Long: "Tear a background tunnel down: drop the port-forward, cancel the job, and forget\n" +
			"the record. Name the job (as `mu job tunnel ls` shows it) or pass --all.\n\n" +
			"--keep-job detaches only the forward and leaves the allocation running — for when\n" +
			"you want the job to keep going and will reattach with `mu job tunnel --job <id>`.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var targets []tunnelRec
			switch {
			case all:
				targets = loadTunnels()
			case len(args) == 1:
				t, err := findTunnel(args[0])
				if err != nil {
					return err
				}
				targets = []tunnelRec{t}
			default:
				return usageErr("name a tunnel (see `mu job tunnel ls`) or pass --all")
			}
			if len(targets) == 0 {
				render.Info("no open tunnels")
				return nil
			}
			verb := "close"
			if !keepJob {
				verb = "close and cancel"
			}
			if !yes {
				for _, t := range targets {
					render.Detail(fmt.Sprintf("%-6s %s  %s  job %s", t.ID, t.URL(), t.System, t.Job))
				}
				fmt.Fprintf(os.Stderr, "%s %d tunnel(s)? [y/N] ", verb, len(targets))
				var r string
				_, _ = fmt.Scanln(&r)
				if strings.ToLower(strings.TrimSpace(r)) != "y" {
					render.Info("aborted")
					return nil
				}
			}
			for _, t := range targets {
				closeTunnel(t, keepJob)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "close every open tunnel")
	c.Flags().BoolVar(&keepJob, "keep-job", false, "drop the forward but leave the job running")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return c
}

// closeTunnel does the teardown for one record: cancel the job (unless keeping it), drop the
// ssh master (which takes the forward with it and frees the local port), forget the record.
// Each step is best-effort and reported — a half-open tunnel is worse than a loud failure.
func closeTunnel(t tunnelRec, keepJob bool) {
	if !keepJob {
		scheduler := config.SchedulerFor(t.System)
		if cmd := cancelCmd(scheduler, []string{t.Job}); cmd != "" {
			if err := hpc.EnsureTicket(); err != nil {
				render.Warn(fmt.Sprintf("%s: no ticket to cancel job %s: %v", t.System, t.Job, err))
			} else if _, err := hpc.RemoteExec(t.Target, cmd); err != nil {
				render.Warn(fmt.Sprintf("%s: cancel job %s failed: %v (cancel it with `mdel %s`)", t.System, t.Job, err, t.Job))
			}
		}
		// mu pushed this script, and the cancelled job no longer needs it — reap the staged copy.
		// Only on a real cancel: --keep-job leaves the allocation running, which is still reading it.
		if t.Staged {
			rm := fmt.Sprintf(`rm -f "$HOME/.local/state/mayhl_utils/jobs/%s.sh"`, t.ID)
			if _, err := hpc.RemoteExec(t.Target, rm); err != nil {
				render.Warn(fmt.Sprintf("%s: couldn't remove the staged script for %s: %v", t.System, t.ID, err))
			}
		}
	}
	// -O exit stops the master; the forward is a channel on it, so it dies too, and the local
	// port is free the instant the socket closes.
	_ = exec.Command(config.SSHCommand(), "-q", "-S", t.Sock, "-O", "exit", t.Target).Run()
	_ = os.Remove(t.Sock)
	forgetTunnel(t)
	if keepJob {
		render.OK(fmt.Sprintf("detached tunnel %s on %s (job %s still running — reattach: `mu job tunnel --job %s -N %s`)", t.ID, t.System, t.Job, t.Job, t.System))
		return
	}
	render.OK(fmt.Sprintf("closed tunnel %s on %s and cancelled job %s", t.ID, t.System, t.Job))
}
