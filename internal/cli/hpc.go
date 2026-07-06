package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

const probeTimeout = 2 * time.Second

// collateTimeout bounds each cluster's fetch during --all fan-out — long enough for
// ssh + Kerberos + login-profile + squeue, short enough that a wedged cluster
// doesn't stall the whole collate.
const collateTimeout = 20 * time.Second

func hpcCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "hpc",
		Short: "Cross-cluster HPC info (nodes, reachability, ticket).",
		Long: "Aggregate info across the configured clusters. Local-primary — run it from your\n" +
			"workstation to reach every cluster; on a login node you'll only see what's\n" +
			"reachable from there.",
	}
	c.AddCommand(hpcNodesCmd(), hpcQueueCmd(), hpcTicketCmd())
	return c
}

func hpcQueueCmd() *cobra.Command {
	var node, userList string
	var allUsers, jsonOut, local, fleet, all, start bool
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
				render.JobsTable(label, config.User(), toJobRows(jobs), start)
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
	c.Flags().BoolVar(&jsonOut, "json", false, "emit jobs as JSON (complete, untruncated) instead of a table")
	c.MarkFlagsMutuallyExclusive("node", "local", "fleet", "all-systems")
	c.MarkFlagsMutuallyExclusive("all-users", "user") // both pick WHO; -u is a subset, -a is everyone
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	return c
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

// clusterResult is one cluster's collate outcome: its jobs, or the error that
// dropped it (unreachable, timed out, misconfigured).
type clusterResult struct {
	cluster string
	jobs    []queue.Job
	err     error
}

// queueTarget is one collate fetch: which node to run the scheduler on and the label
// each returned job is tagged with. label is the cluster name (cluster scopes) or the
// node/system name (an explicit `fleet` list), so a DSRC split across separate schedulers
// stays distinguishable in the merged table.
type queueTarget struct {
	label     string
	scheduler string
	node      string // node name to resolve + fetch from ("" → no node configured)
}

// clusterTargets picks one representative node (Nodes[0]) per cluster — the scope used by
// --all and by --fleet's fallback when no explicit `fleet` list is configured. Clusters
// with no node/scheduler are kept so fetchTarget reports them as a warning, not a silent drop.
func clusterTargets(cs []config.Cluster) []queueTarget {
	t := make([]queueTarget, 0, len(cs))
	for _, c := range cs {
		node := ""
		if len(c.Nodes) > 0 {
			node = c.Nodes[0]
		}
		t = append(t, queueTarget{label: c.Name, scheduler: c.Scheduler, node: node})
	}
	return t
}

// fleetTargets builds one target per node in the explicit `fleet` list, each labeled by
// its node/system name and carrying that node's cluster-declared scheduler.
func fleetTargets(nodes []string) []queueTarget {
	t := make([]queueTarget, 0, len(nodes))
	for _, n := range nodes {
		t = append(t, queueTarget{label: n, scheduler: config.SchedulerFor(n), node: n})
	}
	return t
}

// fleetScope resolves the --fleet target set: the explicit `fleet` node list when
// configured (one fetch per listed system — so a multi-scheduler DSRC like navy isn't
// collapsed to a single representative node), else a soft fallback to one node per active
// cluster (never worse than the prior behavior).
func fleetScope() []queueTarget {
	if nodes := config.Fleet(); len(nodes) > 0 {
		return fleetTargets(nodes)
	}
	return clusterTargets(config.ActiveClusters())
}

// allSystemsScope resolves --all-systems: every distinct queue = the fleet list plus one
// representative node for each configured cluster (incl. inactive) whose nodes are NOT
// already covered by the fleet. A proper superset of --fleet that reaches unlisted/inactive
// clusters without re-querying a cluster's shared scheduler. With no `fleet` list it reduces
// to one node per cluster (the prior --all behavior).
func allSystemsScope() []queueTarget {
	fleet := config.Fleet()
	inFleet := make(map[string]bool, len(fleet))
	for _, n := range fleet {
		inFleet[n] = true
	}
	targets := fleetTargets(fleet)
	for _, c := range config.ClusterDefs() {
		covered := false
		for _, n := range c.Nodes {
			if inFleet[n] {
				covered = true
				break
			}
		}
		if covered {
			continue // a fleet node already fetches this cluster's queue
		}
		node := ""
		if len(c.Nodes) > 0 {
			node = c.Nodes[0]
		}
		targets = append(targets, queueTarget{label: c.Name, scheduler: c.Scheduler, node: node})
	}
	return targets
}

// collateJobs fans out over the given targets concurrently, each fetched with a bounded
// timeout, and returns the display label plus the merged jobs (tagged by label) and
// "label: reason" notes for any that failed — so a down target degrades to a warning,
// never a hang or a total failure. The Kerberos ticket is ensured once up front. scope is
// "fleet" or "all", driving both the label and the empty-set message.
func collateJobs(targets []queueTarget, scope string, who userSel) (string, []queue.Job, []string) {
	if len(targets) == 0 {
		if scope == "fleet" {
			render.Warn("nothing in the fleet — set a `fleet = [...]` node list or `active = true` on a cluster, or use --all-systems")
		} else {
			render.Warn("no clusters configured — add clusters to config.toml")
		}
		os.Exit(2)
	}
	hpc.EnsureTicket()
	results := make([]clusterResult, len(targets))
	var wg sync.WaitGroup
	for i := range targets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = fetchTarget(targets[i], who)
		}(i)
	}
	wg.Wait()
	label := scope
	if scope == "all" {
		label = "all systems"
	}
	jobs, down := mergeResults(results)
	return label, jobs, down
}

// fetchTarget runs one target's scheduler over the bounded remote-exec, tagging each job
// with the target's label.
func fetchTarget(t queueTarget, who userSel) clusterResult {
	if t.node == "" {
		return clusterResult{t.label, nil, errors.New("no nodes configured")}
	}
	cmd, parse := fetchSpec(t.scheduler, who)
	if cmd == "" {
		return clusterResult{t.label, nil, errors.New("no scheduler configured")}
	}
	target, err := hpc.Resolve(t.node)
	if err != nil {
		return clusterResult{t.label, nil, err}
	}
	out, err := hpc.RemoteExecTimeout(target, cmd, collateTimeout)
	if err != nil {
		return clusterResult{t.label, nil, err}
	}
	jobs := parse(out)
	for i := range jobs {
		jobs[i].Cluster = t.label
	}
	return clusterResult{t.label, jobs, nil}
}

// mergeResults flattens per-cluster results into one job list (in cluster order) plus
// "cluster: reason" notes for the failures. Pure — the fan-out's testable core.
func mergeResults(results []clusterResult) ([]queue.Job, []string) {
	var jobs []queue.Job
	var down []string
	for _, r := range results {
		if r.err != nil {
			down = append(down, fmt.Sprintf("%s: %v", r.cluster, r.err))
			continue
		}
		jobs = append(jobs, r.jobs...)
	}
	return jobs, down
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
// carries a login-node number (e.g. login01) absent from config, it retries the
// digit-stripped base (login). A non-empty name with an empty scheduler means
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
			Start: j.Start, Cluster: j.Cluster,
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
