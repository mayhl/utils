package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

const probeTimeout = 2 * time.Second

func hpcCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "hpc",
		Short: "Cross-cluster HPC info (nodes, reachability, ticket).",
		Long: "Aggregate info across the configured HPC clusters. Local-primary — run it from your\n" +
			"workstation to reach every cluster; on a login node you'll only see what's\n" +
			"reachable from there.",
	}
	c.AddCommand(hpcNodesCmd(), hpcQueueCmd(), hpcTicketCmd())
	return c
}

func hpcQueueCmd() *cobra.Command {
	var node string
	var allUsers, jsonOut bool
	c := &cobra.Command{
		Use:   "queue",
		Short: "Render a scheduler queue (PBS qstat / SLURM squeue) as a house table.",
		Long: "Show a cluster's jobs as one normalized house table, regardless of\n" +
			"scheduler. With --node, mu fetches it over remote-exec — picking qstat vs\n" +
			"squeue from the cluster's configured scheduler:\n" +
			"    mu hpc queue --node hpc1        # your jobs\n" +
			"    mu hpc queue --node hpc1 -a     # all users\n\n" +
			"On a login node with no --node and no pipe, mu runs the current cluster's\n" +
			"scheduler locally — this is the `mstat` front-door. Otherwise it reads a\n" +
			"listing from stdin (scheduler auto-detected) — the test/pipe-your-own seam:\n" +
			"    hpc1 squeue | mu hpc queue\n\n" +
			"Cross-cluster collate (bare mstat over the active set) is the next step.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			var jobs []queue.Job
			label := node
			switch {
			case node != "":
				jobs = fetchJobs(node, allUsers)
			case !term.IsTerminal(os.Stdin.Fd()):
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				jobs = queue.Parse(string(data))
			default:
				// No --node and no pipe → fetch the current cluster's queue locally
				// (the on-cluster `mstat` path). Off-HPC this warns and exits.
				label, jobs = fetchJobsLocal(allUsers)
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(jobs)
			}
			render.JobsTable(label, config.User(), toJobRows(jobs))
			return nil
		},
	}
	c.Flags().StringVarP(&node, "node", "N", "", "fetch the queue from this node (else read stdin)")
	c.Flags().BoolVarP(&allUsers, "all-users", "a", false, "all users' jobs (default: yours)")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit jobs as JSON (complete, untruncated) instead of a table")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

// fetchJobs runs the cluster's scheduler command on node over remote-exec and parses
// it into normalized jobs. The scheduler comes from config (not a probe); an
// unconfigured scheduler or a remote failure exits with one house error line.
func fetchJobs(node string, allUsers bool) []queue.Job {
	target, err := hpc.Resolve(node)
	if err != nil {
		render.Err(err.Error())
		os.Exit(2)
	}
	cmd, parse := fetchSpec(config.SchedulerFor(node), allUsers)
	if cmd == "" {
		render.Err(fmt.Sprintf("no scheduler configured for %s — set `scheduler = \"slurm\"|\"pbs\"` on its cluster in config.toml", node))
		os.Exit(2)
	}
	hpc.EnsureTicket()
	out, err := hpc.RemoteExec(target, cmd)
	if err != nil {
		render.Err(fmt.Sprintf("%s: remote fetch failed: %v", node, err))
		os.Exit(1)
	}
	return parse(out)
}

// fetchJobsLocal runs the current cluster's scheduler command locally (no ssh) for
// the on-cluster `mstat` path — you're already on the login node, so mu just runs
// squeue/qstat here. Returns the resolved cluster label + jobs; off-HPC (no current
// cluster) it warns and exits, steering to `<node> mstat` / --node.
func fetchJobsLocal(allUsers bool) (string, []queue.Job) {
	self, scheduler := currentCluster()
	if self == "" {
		render.Warn("not on an HPC cluster — use `<node> mstat` or `mu hpc queue --node <n>`")
		os.Exit(2)
	}
	cmd, parse := fetchSpec(scheduler, allUsers)
	if cmd == "" {
		render.Err(fmt.Sprintf("no scheduler configured for %s — set `scheduler = \"slurm\"|\"pbs\"` on its cluster in config.toml", self))
		os.Exit(2)
	}
	// Same command as the remote fetch, run in a local shell (bash for the quoted
	// -o format arg); the login shell already has the scheduler on PATH.
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		render.Err(fmt.Sprintf("%s: local queue fetch failed: %v", self, err))
		os.Exit(1)
	}
	return self, parse(string(out))
}

// currentCluster resolves the cluster this shell runs on to its (name, scheduler)
// from config, or ("", "") off-HPC. $MU_NODE overrides $BC_HOST; when $BC_HOST
// carries a login-node number (login-a01) absent from config, it retries the
// digit-stripped base (login-a). A non-empty name with an empty scheduler means
// on-HPC-but-unconfigured — the caller reports that.
func currentCluster() (string, string) {
	self := os.Getenv("MU_NODE")
	if self == "" {
		self = os.Getenv("BC_HOST")
	}
	if self == "" {
		return "", ""
	}
	if s := config.SchedulerFor(self); s != "" {
		return self, s
	}
	if base := strings.TrimRight(self, "0123456789"); base != self {
		if s := config.SchedulerFor(base); s != "" {
			return base, s
		}
	}
	return self, ""
}

// fetchSpec returns the remote command + matching parser for a scheduler. SLURM uses
// mu's controlled pipe-delimited format (adds walltime, robust parse); PBS uses the
// wide `qstat -a` (its default already carries Req'd Time). "" cmd = unknown scheduler.
func fetchSpec(scheduler string, allUsers bool) (string, func(string) []queue.Job) {
	switch scheduler {
	case "slurm":
		sel := ""
		if !allUsers {
			sel = "--me "
		}
		return `squeue -h ` + sel + `-o "%i|%P|%j|%u|%t|%M|%l|%D|%R"`, queue.ParseSLURMDelim
	case "pbs":
		cmd := "qstat -a"
		if !allUsers {
			if u := config.HPCUser(); u != "" {
				cmd = "qstat -a -u " + u
			}
		}
		return cmd, queue.ParsePBS
	default:
		return "", nil
	}
}

// toJobRows maps normalized jobs to render's plain JobRow (keeping render domain-
// free). An unknown state shows the raw scheduler code so nothing is silently hidden.
func toJobRows(jobs []queue.Job) []render.JobRow {
	rows := make([]render.JobRow, len(jobs))
	for i, j := range jobs {
		state := j.State.String()
		if j.State == queue.Unknown {
			state = strings.TrimSpace(j.RawState)
		}
		rows[i] = render.JobRow{
			ID: j.ShortID, Name: j.Name, User: j.User, Queue: j.Queue, Nodes: j.Nodes,
			State: state, Elapsed: j.Elapsed, ReqWall: j.ReqWall, Reason: j.PendingReason(),
		}
	}
	return rows
}

func hpcTicketCmd() *cobra.Command {
	var renew bool
	c := &cobra.Command{
		Use:   "ticket",
		Short: "Show local Kerberos ticket status; --renew runs pkinit (local only).",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if renew {
				renewTicket()
			}
			showTicket()
			return nil
		},
	}
	c.Flags().BoolVar(&renew, "renew", false, "obtain/refresh the ticket via pkinit (local only)")
	return c
}

func showTicket() {
	info, available := hpc.Ticket()
	if !available {
		render.Warn("klist not found — no local Kerberos here")
		return
	}
	if !info.Present {
		render.Warn("no Kerberos ticket — run `mu hpc ticket --renew`")
		return
	}
	who := info.Principal
	if who == "" {
		who = "(unknown principal)"
	}
	if !info.Expires.IsZero() {
		rem := time.Until(info.Expires)
		if rem <= 0 {
			render.Warn(fmt.Sprintf("ticket EXPIRED for %s — run `mu hpc ticket --renew`", who))
			return
		}
		render.OK(fmt.Sprintf("ticket: %s   expires %s (in %s)", who, info.Expires.Format("Jan 2 15:04"), humanDur(rem)))
		return
	}
	render.OK("ticket: " + who)
}

func renewTicket() {
	if os.Getenv("BC_HOST") != "" || os.Getenv("MU_SYSTEM") == "hpc" {
		render.Warn("--renew is local-only; on HPC the ticket is inherited from login")
		return
	}
	user := config.HPCUser()
	if user == "" {
		render.Err("no HPC username configured (hpc_user / MU_HPC_UNAME)")
		return
	}
	render.Info("running pkinit for " + user + "…")
	cmd := exec.Command("pkinit", user)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = cmd.Run()
}

// humanDur formats a ticket's remaining life compactly (2d 3h / 5h 47m / 47m).
func humanDur(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h >= 24:
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func hpcNodesCmd() *cobra.Command {
	var status bool
	c := &cobra.Command{
		Use:   "nodes",
		Short: "List configured nodes; -s probes ssh reachability from here.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			defs := config.ClusterDefs()
			if len(defs) == 0 {
				render.Warn("no nodes — is the cluster config set?")
				os.Exit(1)
			}
			var st map[string]string
			if status {
				hosts := make(map[string]string)
				for _, cl := range defs {
					for _, n := range cl.Nodes {
						hosts[n] = n + "." + cl.Domain
					}
				}
				st = hpc.Probe(hosts, probeTimeout)
			}
			render.NodesTable(defs, config.User(), st)
			return nil
		},
	}
	c.Flags().BoolVarP(&status, "status", "s", false, "probe ssh (port 22) reachability from here — ● up / ○ down")
	return c
}
