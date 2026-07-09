package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

func hpcQueueCmd() *cobra.Command {
	var node, userList string
	var allUsers, jsonOut, local, fleet, all, start, interactive bool
	c := &cobra.Command{
		Use:   "queue",
		Short: "Render a scheduler queue (PBS qstat / SLURM squeue) as a house table.",
		Long: "Show cluster jobs as one normalized house table, regardless of scheduler\n" +
			"(PBS qstat / SLURM squeue). Three WHERE scopes select how wide to look\n" +
			"(orthogonal to WHO: default you, -u alice,bob specific users, -a everyone):\n\n" +
			"    -l --local         current cluster only, run locally — no ssh (default on HPC)\n" +
			"    -f --fleet         the `fleet` node list, else active clusters (default off HPC)\n" +
			"    -e --all-systems   every distinct queue: the fleet plus one node per cluster\n" +
			"                       not already in it (incl. inactive)\n\n" +
			"Bare `mstat` resolves by location: on a login node it's --local; off HPC,\n" +
			"where there's no local scheduler, it's --fleet. --fleet/--all-systems fan out\n" +
			"concurrently with a per-node timeout, tagging each job with a System\n" +
			"column; an unreachable system degrades to a warning, never a hang:\n" +
			"    mstat                          # local on HPC, fleet off it\n" +
			"    mstat -f -a                    # the fleet, all users\n" +
			"    mstat -e -a                    # every system, all users\n\n" +
			"--node fetches one cluster over remote-exec (qstat vs squeue from its\n" +
			"configured scheduler); with neither --node nor a scope flag, a listing piped\n" +
			"on stdin is parsed (scheduler auto-detected) — the test/pipe-your-own seam:\n" +
			"    mu hpc queue --node hpc1\n" +
			"    hpc1 squeue | mu hpc queue",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if userList != "" && !validUserList(userList) {
				render.Err("--user takes a comma-separated user list (letters/digits/._-), e.g. -u alice,bob")
				os.Exit(2)
			}
			who := userSel{all: allUsers, list: userList}
			if interactive {
				if fleet || all {
					render.Err("mstat -i is single-cluster — drop -f/-e (use --node for another cluster)")
					os.Exit(2)
				}
				return mstatInteractive(node, who)
			}
			var jobs []queue.Job
			var down []string
			var label string
			switch {
			case node != "":
				label = node
				jobs = fetchJobs(node, who)
			case all:
				label, jobs, down = collateJobs(allSystemsScope(), "all", who)
			case fleet:
				label, jobs, down = collateJobs(fleetScope(), "fleet", who)
			case local:
				// Explicit --local: current cluster, run locally. Off-HPC this warns
				// and exits (no scheduler here) rather than silently widening.
				label, jobs = fetchJobsLocal(who)
			case !term.IsTerminal(os.Stdin.Fd()):
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				jobs = queue.Parse(string(data))
			default:
				// Bare mstat, no pipe → resolve by location: on a login node run the
				// current cluster locally; off HPC (no local scheduler) fall to fleet.
				if self, _ := currentCluster(); self != "" {
					label, jobs = fetchJobsLocal(who)
				} else {
					label, jobs, down = collateJobs(fleetScope(), "fleet", who)
				}
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(jobs); err != nil {
					return err
				}
			} else {
				render.JobsTable(label, config.User(), toJobRows(jobs), render.JobCols{Start: start})
			}
			for _, d := range down { // unreachable clusters degrade to warnings, never a hang
				render.Warn(d)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&node, "node", "N", "", "fetch the queue from this node (else read stdin)")
	c.Flags().BoolVarP(&allUsers, "all-users", "a", false, "all users' jobs (default: yours)")
	c.Flags().StringVarP(&userList, "user", "u", "", "show these users' jobs (comma-separated), e.g. -u alice,bob")
	c.Flags().BoolVarP(&local, "local", "l", false, "current cluster only, fetched locally (default on HPC)")
	c.Flags().BoolVarP(&fleet, "fleet", "f", false, "collate the `fleet` node list, else active clusters (default off HPC)")
	c.Flags().BoolVarP(&all, "all-systems", "e", false, "collate every distinct queue: the fleet plus one node per cluster not in it, incl. inactive")
	c.Flags().BoolVar(&start, "start", false, "add a Start column: actual start (running) or estimated start (pending); SLURM only")
	c.Flags().BoolVarP(&interactive, "interactive", "i", false, "pick jobs to cancel interactively (single cluster)")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit jobs as JSON (complete, untruncated) instead of a table")
	c.MarkFlagsMutuallyExclusive("node", "local", "fleet", "all-systems")
	c.MarkFlagsMutuallyExclusive("all-users", "user") // both pick WHO; -u is a subset, -a is everyone
	c.AddCommand(queueKillCmd(), queueInfoCmd(), queuePeekCmd(), queueHoldCmd(), queueReleaseCmd(), queueHistCmd())
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

// errNoScheduler reports an unconfigured scheduler for a cluster and exits(2) — the shared
// exit path for the queue verbs when config carries no scheduler = "slurm"|"pbs".
func errNoScheduler(label string) {
	render.Err(fmt.Sprintf("no scheduler configured for %s — set `scheduler = \"slurm\"|\"pbs\"` in config.toml", label))
	os.Exit(2)
}

// fetchJobs runs the cluster's scheduler command on node over remote-exec and parses
// it into normalized jobs. The scheduler comes from config (not a probe); an
// unconfigured scheduler or a remote failure exits with one house error line.
func fetchJobs(node string, who userSel) []queue.Job {
	target, err := hpc.Resolve(node)
	if err != nil {
		render.Err(err.Error())
		os.Exit(2)
	}
	cmd, parse := fetchSpec(config.SchedulerFor(node), who)
	if cmd == "" {
		errNoScheduler(node)
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
func fetchJobsLocal(who userSel) (string, []queue.Job) {
	self, scheduler := currentCluster()
	if self == "" {
		render.Warn("not on an HPC cluster — use `<node> mstat` or `mu hpc queue --node <n>`")
		os.Exit(2)
	}
	cmd, parse := fetchSpec(scheduler, who)
	if cmd == "" {
		errNoScheduler(self)
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

// userSel is the WHO axis of a fetch: whose jobs to show. Exactly one applies, in
// precedence order — an explicit list (-u), then all users (-a), else just you.
type userSel struct {
	all  bool   // -a / --all-users: no user filter
	list string // -u / --user: comma-separated user list; "" = unset
}

// validUserList guards the -u value: it is interpolated into the remote/local shell
// command, so restrict it to the characters a username list can contain (comma is the
// separator) — no spaces or shell metacharacters.
func validUserList(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ',' || r == '_' || r == '.' || r == '-':
		default:
			return false
		}
	}
	return true
}

// fetchSpec returns the remote command + matching parser for a scheduler. SLURM uses
// mu's controlled pipe-delimited format (adds walltime, robust parse); PBS uses the
// wide `qstat -a` (its default already carries Req'd Time). The WHO axis picks the user
// filter: a -u list, all users (-a), or just you. "" cmd = unknown scheduler.
func fetchSpec(scheduler string, who userSel) (string, func(string) []queue.Job) {
	switch scheduler {
	case "slurm":
		sel := "--me " // default: your jobs
		switch {
		case who.list != "":
			sel = "-u " + who.list + " "
		case who.all:
			sel = ""
		}
		return `squeue -h ` + sel + `-o "%i|%P|%j|%u|%t|%M|%l|%D|%R|%S"`, queue.ParseSLURMDelim
	case "pbs":
		sel := ""
		switch {
		case who.list != "":
			sel = " -u " + who.list
		case who.all:
			sel = ""
		default:
			if u := config.HPCUser(); u != "" {
				sel = " -u " + u
			}
		}
		return "qstat -a" + sel, queue.ParsePBS
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
			Submit: j.Submit, Start: j.Start, End: j.End, Cluster: j.Cluster,
		}
	}
	return rows
}
