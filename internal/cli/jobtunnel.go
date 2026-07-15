package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/shell"
)

// jobTunnelCmd is `mu job tunnel`: the compute-node tunnel flow in one verb —
// submit a service script (or adopt a running job with --job), poll the
// scheduler until it runs and its node is known, then hold an ssh -L tunnel
// through the login node until Ctrl-C (compute nodes aren't reachable from the
// workstation; the login node relays). -I instead allocates an interactive
// shell (qsub -I / salloc) under a real tty.
func jobTunnelCmd() *cobra.Command {
	var node, jobID, account, walltime, name string
	var sel queueSel
	var port, localPort int
	var yes, interactive, foreground bool
	var wait, poll time.Duration
	c := &cobra.Command{
		Use:   "tunnel [script]",
		Short: "Submit a job and tunnel a port to its compute node.",
		Long: "The service-tunnel flow: submit <script> (something that serves a port —\n" +
			"jupyter, a dashboard) on the target cluster, wait until the scheduler reports\n" +
			"it running and names its node, then open localhost:<-l> → <node>:<-p> through\n" +
			"the login node and hold it until Ctrl-C — the job keeps running; reattach\n" +
			"with --job <id>, which also adopts any already-submitted job instead of\n" +
			"submitting. One held connection carries the whole flow. For an interactive\n" +
			"shell on a compute node see `mu job shell`. Front-door: `mtunnel`.\n\n" +
			"-i opens the tunnel form instead: script/job, queue, account, the ports and\n" +
			"the run mode (background/foreground) as editable fields, pre-seeded from the\n" +
			"flags and config, with the queue enum backed by the cluster's queue list. The\n" +
			"usual preview + confirm still follows.\n\n" +
			"    mu job tunnel ~/serve.sh -N hpc1 -p 8888\n" +
			"    mu job tunnel --job 4501 -N hpc1 -p 8888 -l 9999",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			script := ""
			if len(args) == 1 {
				script = args[0]
			}
			if interactive {
				if !render.Interactive() {
					return usageErr("mu job tunnel -i needs a terminal (stdin is not a tty)")
				}
				if node == "" {
					return usageErr("needs -N <cluster> — the tunnel runs from the workstation")
				}
				// The form's queue fetch is remote — get the ticket BEFORE the TUI owns the terminal.
				if err := hpc.EnsureTicket(); err != nil {
					return runErr("%s", err)
				}
				if account == "" {
					account = config.AccountFor(node)
				}
				f, ok, err := tunnelForm(node, node, script, jobID, account, walltime, &sel, port, localPort, foreground)
				if err != nil {
					return err
				}
				if !ok {
					render.Info("aborted")
					return nil
				}
				// The form validated the exclusivity and the port; its queue is now literal.
				script, jobID, account, walltime = f.Script, f.JobID, f.Account, f.Walltime
				port, localPort = f.Port, f.LocalPort
				foreground = f.Foreground
				sel = queueSel{queue: f.Queue}
				return jobTunnel(node, script, jobID, account, walltime, &sel, port, localPort, name, foreground, yes, wait, poll)
			}
			if script == "" && jobID == "" {
				return usageErr("tunnel needs a <script> to submit or --job <id> (or -i for the form)")
			}
			if script != "" && jobID != "" {
				return usageErr("<script> and --job are exclusive — submit or adopt, not both")
			}
			if port == 0 {
				return usageErr("needs -p <port> — the service port on the compute node")
			}
			if localPort == 0 {
				localPort = port
			}
			return jobTunnel(node, script, jobID, account, walltime, &sel, port, localPort, name, foreground, yes, wait, poll)
		},
	}
	setHelpArgs(c, [2]string{"[script]", "service script: a local path is pushed, a remote path submitted as-is"})
	f := c.Flags()
	f.StringVarP(&node, "node", "N", "", "cluster to target (required off an HPC login node)")
	f.StringVar(&jobID, "job", "", "adopt this already-submitted job instead of submitting")
	f.IntVarP(&port, "port", "p", 0, "service port on the compute node")
	f.IntVarP(&localPort, "local", "l", 0, "local port to listen on (default: same as --port)")
	f.StringVarP(&account, "account", "A", "", "allocation to charge (overrides the cluster's config default)")
	addQueueSelFlags(c, &sel)
	f.StringVarP(&walltime, "walltime", "t", "", "how long to hold the job: HH:MM:SS or a duration (10m, 1h, 1.5h); default: config interactive_walltime")
	f.StringVarP(&name, "name", "J", "", "job name (default: an opaque mu-<id>)")
	f.BoolVar(&foreground, "fg", false, "hold the tunnel in the foreground (default: background — mu exits once it's up)")
	f.BoolVarP(&interactive, "interactive", "i", false, "edit the tunnel in a form (fields pre-seeded from flags + config, live queue list)")
	f.BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	f.DurationVar(&wait, "wait", 15*time.Minute, "give up if the job isn't running by then")
	f.DurationVar(&poll, "poll", 5*time.Second, "scheduler poll interval while waiting")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	c.AddCommand(tunnelLsCmd(), tunnelCloseCmd())
	return c
}

// jobShellCmd is `mu job shell`: an interactive allocation on a compute node —
// the scheduler's own qsub -I / salloc under a real tty (RemoteExec is tty-less
// by design, so this path builds its own `ssh -t`). The tunnel's sibling: shell
// = you on the node, tunnel = a service's port. FUTURE: -p adds a tunnel to the
// allocated node once the scheduler names it (the mux makes that composable).
func jobShellCmd() *cobra.Command {
	var node, account, walltime string
	var nodes int
	var sel queueSel
	var interactive bool
	c := &cobra.Command{
		Use:   "shell",
		Short: "Open an interactive allocation on a compute node (qsub -I / salloc).",
		Long: "Request an interactive allocation through the target's scheduler and hand you\n" +
			"the shell on the compute node, tty and all. Exiting the shell releases the\n" +
			"allocation. For tunnelling a service's port instead, see `mu job tunnel`.\n" +
			"Front-door: `mshell`.\n\n" +
			"-i picks the queue and account in a form, its queue enum backed by the\n" +
			"cluster's queue list — the way to see what you can actually allocate on.\n\n" +
			"    mu job shell -N hpc1 --debug",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if interactive {
				if !render.Interactive() {
					return usageErr("mu job shell -i needs a terminal (stdin is not a tty)")
				}
				label, _, _, _, _, err := queueTargetCtx(node, userSel{})
				if err != nil {
					return err
				}
				if node != "" { // the form's queue fetch is remote — ticket BEFORE the TUI takes the terminal
					if err := hpc.EnsureTicket(); err != nil {
						return runErr("%s", err)
					}
				}
				if account == "" {
					account = config.AccountFor(label)
				}
				q, acct, wall, n, ok, err := shellForm(node, label, account, walltime, nodes, &sel)
				if err != nil {
					return err
				}
				if !ok {
					render.Info("aborted")
					return nil
				}
				account, walltime, nodes = acct, wall, n
				sel = queueSel{queue: q} // the form's pick is literal — don't re-resolve a class flag
			}
			return jobInteractive(node, account, walltime, nodes, &sel)
		},
	}
	c.Flags().StringVarP(&node, "node", "N", "", "cluster to target (required off an HPC login node)")
	c.Flags().StringVarP(&account, "account", "A", "", "allocation to charge (overrides the cluster's config default)")
	addQueueSelFlags(c, &sel)
	c.Flags().StringVarP(&walltime, "walltime", "t", "", "how long to hold the session: HH:MM:SS or a duration (10m, 1h, 1.5h); default: config interactive_walltime")
	c.Flags().IntVarP(&nodes, "nodes", "n", 1, "nodes to allocate (PBS select chunk / SLURM -N)")
	c.Flags().BoolVarP(&interactive, "interactive", "i", false, "pick the queue, account and walltime in a form (queue enum from the cluster's queue list)")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

// jobTunnel is the script-mode pipeline over ONE held connection: open an ssh
// ControlMaster (the single auth), submit and wait as channels on it, then add
// the port-forward to the same live connection and hold until Ctrl-C. The
// login node sees one session for the whole flow.
func jobTunnel(node, script, jobID, account, walltime string, sel *queueSel, port, localPort int, name string, foreground, yes bool, wait, poll time.Duration) error {
	if node == "" {
		return usageErr("needs -N <cluster> — the tunnel runs from the workstation")
	}
	// Bind the local end BEFORE anything remote: a doomed port shouldn't cost a submit. When
	// -l was given it must be that port or an error; otherwise mu picks the first free one at
	// or above the remote port, so the URL stays predictable.
	localPort, err := pickLocalPort(localPort, port)
	if err != nil {
		return err
	}
	scheduler := config.SchedulerFor(node)
	adapter := queue.For(scheduler)
	if adapter == nil {
		return errNoScheduler(node)
	}
	startedAt := time.Now()
	target, err := hpc.Resolve(node)
	if err != nil {
		return usageErr("%s", err)
	}
	if account == "" {
		account = config.AccountFor(node)
	}
	queue_, wall := "", ""
	if jobID == "" { // adopt mode never submits — don't resolve (or live-fetch) a queue for it
		if queue_, err = sel.resolve(node, node, false); err != nil {
			return err
		}
		// A tunnel is a HELD session: it lives exactly as long as its job, so the config
		// default applies — unless the script speaks for itself.
		dflt := ""
		if mayInjectWalltime(script) {
			if dflt, err = interactiveWalltime(node); err != nil {
				return err
			}
		}
		debugMax := (sel.debug || sel.dbg) && mayInjectWalltime(script)
		if wall, err = resolveWalltime(node, queue_, walltime, dflt, debugMax); err != nil {
			return err
		}
	}
	part, qos := submitTarget(node, queue_)
	id := newTunnelID()
	// The job wears mu-<id> — no port, no "tunnel" — so a cluster-wide qstat leaks nothing.
	// -J still overrides for someone who wants a name of their own; the id remains the handle.
	if name == "" {
		name = jobName(id)
	}
	// Hand the job its port so the service and the forward agree by construction — the trap
	// otherwise is a script that hardcodes one number while -p names another.
	opts := queue.SubmitOpts{
		Account: account, Queue: part, QOS: qos, Walltime: wall, Name: name,
		Env: map[string]string{"MU_PORT": strconv.Itoa(port)},
	}
	// A LOCAL script is pushed to the cluster and submitted from there — so `~/serve.sh` names
	// the file on YOUR disk, the way tab-completion already resolved it — while a bare remote
	// path is submitted as-is. The staged path is deterministic from the id, so the submit
	// command below is honest before the file is actually written (which needs the mux).
	push := jobID == "" && isLocalScript(script)
	remoteScript := script
	if push {
		remoteScript = stagedPath(id)
	}
	submitCmd := adapter.SubmitCmd(remoteScript, opts)

	render.Info(fmt.Sprintf("Tunnel job → %s (%s)", node, scheduler))
	if jobID == "" {
		if push {
			render.Verbose(fmt.Sprintf("script:  %s → %s (push)", script, remoteScript))
		} else {
			render.Verbose("script:  " + remoteScript)
		}
		render.Detail("command: " + submitCmd)
	} else {
		render.Detail("job:     " + jobID)
	}
	render.Verbose(fmt.Sprintf("tunnel:  localhost:%d → <node>:%d, one held connection", localPort, port))
	if !foreground {
		render.Verbose("mode:    background — mu exits once it's up; close with `mu job tunnel close`")
	}
	if !yes {
		fmt.Fprintf(os.Stderr, "connect + tunnel on %s? [y/N] ", node)
		var r string
		_, _ = fmt.Scanln(&r)
		if strings.ToLower(strings.TrimSpace(r)) != "y" {
			render.Info("aborted")
			return nil
		}
	}
	if err := hpc.EnsureTicket(); err != nil {
		return runErr("%s", err)
	}

	mux, err := startMux(target, !foreground, fmt.Sprintf("%s-%d", node, localPort))
	if err != nil {
		return runErr("%s: connect: %s", node, err)
	}
	// A backgrounded master must SURVIVE mu's exit — that's the point. But it must NOT survive a
	// FAILED setup: an error after the master is up (bad submit, never-runs, forward refused)
	// would otherwise orphan the master + its control socket, and the next run trips over the
	// stale socket. So tear it down on every exit EXCEPT the successful background return, which
	// arms keepMux just before it returns. Foreground always closes here.
	keepMux := false
	defer func() {
		if !keepMux {
			mux.close()
		}
	}()
	// Ctrl-C anywhere in the flow: closing the mux collapses whatever leg is
	// in flight (its channel dies with the master) — flows unwind naturally.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	aborted := make(chan struct{})
	go func() {
		<-sig
		close(aborted)
		mux.close()
	}()

	if push {
		// Write the local script to its staged path over the master we already hold — before
		// submit, so a failed push aborts cleanly (keepMux stays false → the master is torn down).
		if _, err := writeStaged(mux.run, script, id); err != nil {
			return tunnelErr(aborted, "%s: %s", node, err)
		}
		render.OK("pushed " + script + " → " + remoteScript)
	}

	if jobID == "" {
		out, err := mux.run(submitCmd)
		if err != nil {
			return tunnelErr(aborted, "%s: submit: %s", node, err)
		}
		if jobID = queue.ParseSubmitID(scheduler, out); jobID == "" {
			return runErr("%s: submit returned no job id:\n%s", node, strings.TrimSpace(out))
		}
		render.OK("submitted " + jobID)
	}

	host, err := waitRunning(mux.run, adapter, scheduler, jobID, wait, poll)
	if err != nil {
		return tunnelErr(aborted, "%s", err)
	}
	render.OK(fmt.Sprintf("job %s running on %s", jobID, host))

	if err := mux.forward(localPort, host, port); err != nil {
		return runErr("adding the forward: %s", err)
	}

	rec := tunnelRec{
		ID: id, System: node, Job: jobID, Host: host, Target: target, Sock: mux.sock,
		LocalPort: localPort, RemotePort: port, Walltime: wall, Script: remoteScript, Staged: push, Started: startedAt,
	}
	if err := saveTunnel(rec); err != nil {
		// The tunnel is up; we just can't track it. Say so rather than tear down working work.
		render.Warn(fmt.Sprintf("tunnel is up but not recorded (%v) — `close` won't find it; Ctrl-C or `mdel %s`", err, jobID))
	}

	if !foreground {
		keepMux = true // setup succeeded — the master now outlives mu; `close` tears it down later
		render.OK(fmt.Sprintf("tunnel up: %s → %s:%d (background; close with `mu job tunnel close %s`)", rec.URL(), host, port, id))
		return nil
	}
	render.OK(fmt.Sprintf("tunnel up: %s → %s:%d (Ctrl-C to close)", rec.URL(), host, port))
	defer forgetTunnel(rec) // foreground: the tunnel dies with this process, so its record should too
	select {
	case <-aborted:
		render.Info("tunnel closed")
		return nil
	case err := <-mux.death:
		return runErr("connection to %s dropped: %v — resume with --job %s", node, err, jobID)
	}
}

// tunnelErr maps an error after a Ctrl-C to a clean abort (the mux teardown
// makes the in-flight leg fail — that's the teardown working, not a failure).
func tunnelErr(aborted <-chan struct{}, format string, a ...any) error {
	select {
	case <-aborted:
		render.Info("aborted")
		return nil
	default:
		return runErr(format, a...)
	}
}

// waitRunning blocks until the job reports running AND names its node — a job
// can sit in R for a beat before exec_host/BatchHost appears. The poll loop
// runs ON the login node inside ONE ssh session (laptop-side polling would be
// a fresh connection + Kerberos handshake per tick — connection churn a login
// node logs; the local qstat it runs instead is the cheap, intended kind of
// polling). Ctrl-C drops the ssh; the remote loop dies with its terminal.
func waitRunning(capture func(string) (string, error), a queue.Adapter, scheduler, jobID string, wait, poll time.Duration) (string, error) {
	render.Info(fmt.Sprintf("waiting for %s to run (one login-node session, up to %s; Ctrl-C to give up) …", jobID, wait))
	// running / finished markers in the raw detail, per scheduler — matched
	// remotely so the loop needs no mu on the far side; the final detail is
	// parsed HERE by the normal parser.
	runRe, doneRe := `job_state = R`, `job_state = [CEF]`
	hostRe := `exec_host`
	if scheduler == "slurm" {
		runRe, doneRe = `JobState=RUNNING`, `JobState=\(COMPLETED\|FAILED\|CANCELLED\|TIMEOUT\)`
		hostRe = `BatchHost=`
	}
	detail := a.DetailCmd([]string{jobID})
	script := fmt.Sprintf(
		`_d=$((SECONDS+%d)); while [ $SECONDS -lt $_d ]; do `+
			`_o=$(%s 2>/dev/null); `+
			`if printf '%%s' "$_o" | grep -q '%s' && printf '%%s' "$_o" | grep -q '%s'; then printf '%%s' "$_o"; exit 0; fi; `+
			`if printf '%%s' "$_o" | grep -q '%s'; then printf '%%s' "$_o"; exit 0; fi; `+
			`sleep %d; done; echo MU_WAIT_TIMEOUT`,
		int(wait.Seconds()), detail, runRe, hostRe, doneRe, max(1, int(poll.Seconds())),
	)
	out, err := capture(script)
	if err != nil {
		return "", runErr("wait for %s: %s", jobID, err)
	}
	if strings.Contains(out, "MU_WAIT_TIMEOUT") {
		return "", runErr("job %s not running after %s — check the queue (`minfo %s`), then retry with --job %s", jobID, wait, jobID, jobID)
	}
	for _, d := range queue.ParseDetails(scheduler, out) {
		if d.State == "running" && d.ExecHost != "" {
			return d.ExecHost, nil
		}
		if d.State == "complete" || d.State == "exiting" {
			return "", runErr("job %s already finished — nothing to tunnel to", jobID)
		}
	}
	return "", runErr("job %s: could not read a running state from the scheduler detail", jobID)
}

// sshMux is one held ssh connection (OpenSSH ControlMaster): mu owns the
// master process, every leg (submit, wait, forward) is a channel on it — one
// auth handshake, one session on the login node for the whole flow.
type sshMux struct {
	bin, sock, target string
	master            *exec.Cmd
	death             chan error // master exit (connection drop / teardown)
}

// startMux opens the master (auth prompts pass through) and waits until the
// control socket answers. The socket lives in the temp dir — short enough for
// the unix-socket path limit.
//
// persist detaches it: ssh forks into the background (-f) and stays alive after mu exits
// (ControlPersist), which is what makes a BACKGROUNDED tunnel possible at all — the forward
// belongs to the master, so the master has to outlive the process that asked for it. The
// socket is then named for the target and port rather than mu's pid: a pid that no longer
// exists is a poor handle for something you want to close tomorrow.
func startMux(target string, persist bool, id string) (*sshMux, error) {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("mu-tun-%d", os.Getpid()))
	if persist {
		sock = filepath.Join(os.TempDir(), "mu-tun-"+id)
	}
	m := &sshMux{
		bin:    config.SSHCommand(),
		sock:   sock,
		target: target,
		death:  make(chan error, 1),
	}
	// A socket left by a crashed, force-killed, or older-mu master makes ssh refuse to open a
	// new one ("ControlSocket ... already exists, disabling multiplexing"). If nothing live
	// answers it, it's a corpse — reap it so a retry isn't wedged. Only the persist path names
	// the socket for the target (so it can collide); the pid-named path never does.
	if persist {
		if _, err := os.Stat(sock); err == nil {
			if exec.Command(m.bin, "-q", "-S", sock, "-O", "check", target).Run() != nil {
				_ = os.Remove(sock)
			}
		}
	}
	// -q: quiet the login banner the server prints on connect — every other ssh call in this
	// path already passes it; the master was the lone leak. -x: no X11 forwarding. A tunnel
	// never needs it, and a ~/.ssh/config that turns it on makes every channel print "No xauth
	// data; using fake authentication data" — noise on a path whose whole output is three lines.
	args := []string{"-q", "-x", "-M", "-S", m.sock, "-N", "-o", "ConnectTimeout=10"}
	if persist {
		args = append(args, "-f", "-o", "ControlPersist=yes")
	}
	m.master = exec.Command(m.bin, append(args, target)...)
	m.master.Stdin, m.master.Stderr = os.Stdin, os.Stderr // Kerberos/host-key prompts stay answerable
	if err := m.master.Start(); err != nil {
		return nil, err
	}
	if persist {
		// -f means the process we started EXITS once the real master has forked away, so its
		// exit is success, not death. Nothing to watch: the socket check below is the test.
		_ = m.master.Wait()
		m.master = nil
	} else {
		go func() { m.death <- m.master.Wait() }()
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if exec.Command(m.bin, "-q", "-S", m.sock, "-O", "check", target).Run() == nil {
			return m, nil
		}
		select {
		case err := <-m.death:
			return nil, fmt.Errorf("connection failed: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			m.close()
			return nil, fmt.Errorf("control socket not ready after 30s")
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// run executes one remote command as a channel on the held connection — no new
// auth, no new session on the far side.
func (m *sshMux) run(remoteCmd string) (string, error) {
	cmd := exec.Command(m.bin, "-q", "-x", "-S", m.sock, m.target, "bash -lc "+shell.Quote(remoteCmd))
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// forward adds -L localPort:host:port to the LIVE master — the tunnel joins
// the connection that has been open since submit.
func (m *sshMux) forward(localPort int, host string, port int) error {
	return exec.Command(m.bin, "-q", "-x", "-S", m.sock, "-O", "forward",
		"-L", fmt.Sprintf("%d:%s:%d", localPort, host, port), m.target).Run()
}

// close asks the master to exit (which drops every channel and forward) and
// reaps it; the process kill is the belt for a wedged master.
func (m *sshMux) close() {
	_ = exec.Command(m.bin, "-q", "-S", m.sock, "-O", "exit", m.target).Run()
	// The persist path forks the real master away and nils m.master, so `-O exit` above is what
	// actually tears it down; the Kill is only the belt for a non-persist (foreground) master.
	if m.master != nil && m.master.Process != nil {
		_ = m.master.Process.Kill()
	}
}

// jobInteractive replaces mu with `ssh -t <login> <qsub -I|salloc>` — the
// scheduler's own interactive allocation under a real tty (RemoteExec is
// tty-less by design, so this path builds its own ssh).
func jobInteractive(node, account, walltime string, nodes int, sel *queueSel) error {
	label, scheduler, _, _, _, err := queueTargetCtx(node, userSel{})
	if err != nil {
		return err
	}
	adapter := queue.For(scheduler)
	if adapter == nil {
		return errNoScheduler(label)
	}
	target, err := hpc.Resolve(label)
	if err != nil {
		return usageErr("%s", err)
	}
	if err := hpc.EnsureTicket(); err != nil {
		return runErr("%s", err)
	}
	if account == "" {
		account = config.AccountFor(label)
	}
	queue_, err := sel.resolve(node, label, false)
	if err != nil {
		return err
	}
	// No script here at all — the session IS the job, so the config default always applies.
	dflt, err := interactiveWalltime(label)
	if err != nil {
		return err
	}
	wall, err := resolveWalltime(node, queue_, walltime, dflt, sel.debug || sel.dbg)
	if err != nil {
		return err
	}
	part, qos := submitTarget(label, queue_)
	// An interactive session has no script to declare its resources, so mu must — and a PBS
	// site rejects `qsub -I` outright without a select chunk ("Please include number of nodes
	// with -l select"). One node unless asked otherwise; ncpus/mpiprocs come from the
	// machine's cores_per_node, because the same sites that demand the chunk demand it FULL.
	if nodes < 1 {
		nodes = 1
	}
	// Name it mu-<id>, same as a tunnel — a bland handle in the queue rather than the
	// scheduler's anonymous default (or a leaked "interactive"). No registry entry: an
	// interactive shell dies with its terminal, so there's nothing to track or close.
	icmd := adapter.InteractiveCmd(queue.SubmitOpts{
		Account: account, Queue: part, QOS: qos, Walltime: wall, Name: jobName(newTunnelID()),
		Nodes: nodes, CoresPerNode: queueCPN(label, queue_),
	})
	render.Info(fmt.Sprintf("interactive allocation on %s: %s", label, icmd))
	ssh := config.SSHCommand()
	// `bash -lc`, exactly as RemoteExec does it: ssh runs the command in the user's LOGIN
	// SHELL WITHOUT LOGIN SEMANTICS, so /etc/profile.d never runs and the scheduler isn't on
	// PATH — a PBS site answers `command not found: qsub`. (SLURM happened to work only
	// because salloc was already on the default PATH there.) The login shell must be bash,
	// not the user's zsh: the site's profile scripts are written for it.
	//
	// -q silences the client's pre-auth banner (the consent notice); the MOTD and the profile
	// noise come down the pty instead, and allocView drops those.
	cmd := exec.Command(ssh, "-q", "-t", target, "bash -lc "+shell.Quote(icmd))
	view := newAllocView(os.Stdout)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, view, os.Stderr
	// ssh -t puts THIS terminal in raw mode, where a bare \n doesn't return the cursor to
	// column 0 — without this every house line below staircases to the right.
	render.SetCRLF(true)
	defer render.SetCRLF(false)
	err = cmd.Run()
	view.flush()
	if err != nil && !strings.Contains(err.Error(), "signal:") {
		return runErr("interactive session: %s", err)
	}
	return nil
}
