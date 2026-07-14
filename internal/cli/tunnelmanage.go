package cli

import (
	"fmt"
	"os"
	"os/exec"
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
func tunnelLsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ls",
		Short: "List the open background tunnels.",
		Long: "Show the tunnels `mu job tunnel` has opened in the background, each with its URL,\n" +
			"the job behind it and how much walltime is left. A job the scheduler no longer\n" +
			"knows (ended or cancelled) is dropped from the registry as it's noticed.",
		Aliases: []string{"list"},
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			recs := loadTunnels()
			if len(recs) == 0 {
				render.Info("no open tunnels")
				return nil
			}
			rows := make([]render.TunnelRow, 0, len(recs))
			ticketed := false
			for _, t := range recs {
				state, left := "?", t.Walltime
				// One ticket for the whole sweep, and only if there's something to ask.
				if !ticketed {
					if err := hpc.EnsureTicket(); err == nil {
						ticketed = true
					}
				}
				if ticketed {
					if st, rem, live := tunnelLiveState(t); !live {
						forgetTunnel(t) // the job is gone — so is the reason to keep the record
						continue
					} else {
						state, left = st, rem
					}
				}
				rows = append(rows, render.TunnelRow{ID: t.ID, URL: t.URL(), System: t.System, Job: t.Job, Node: t.Host, State: state, WallLeft: left})
			}
			if len(rows) == 0 {
				render.Info("no open tunnels")
				return nil
			}
			render.TunnelsTable(rows)
			return nil
		},
	}
	return c
}

// tunnelLiveState asks the job's scheduler for its state and remaining walltime. live=false
// means the scheduler has no record of it — the job ended, and the tunnel with it.
func tunnelLiveState(t tunnelRec) (state, left string, live bool) {
	scheduler := config.SchedulerFor(t.System)
	cmd := detailCmd(scheduler, []string{t.Job})
	if cmd == "" {
		return "?", t.Walltime, true // no scheduler configured — can't judge, so don't prune
	}
	out, err := hpc.RemoteExec(t.Target, cmd)
	if err != nil {
		return "?", t.Walltime, true // a transient ssh failure is not proof the job is gone
	}
	ds := queue.ParseDetails(scheduler, out)
	if len(ds) == 0 {
		return "", "", false
	}
	d := ds[0]
	return d.State, wallLeft(d.ReqWall, d.Elapsed), true
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
