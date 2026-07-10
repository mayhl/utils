package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	var node, script, jobID, account, queue_ string
	var port, localPort int
	var interactive, yes bool
	var wait, poll time.Duration
	c := &cobra.Command{
		Use:   "tunnel",
		Short: "Submit a job and tunnel a port to its compute node (or -I for a shell).",
		Long: "The compute-node access flow: submit -s <script> (a service — jupyter, a\n" +
			"dashboard) on the target cluster, wait until the scheduler reports it running\n" +
			"and names its node, then open localhost:<-l> → <node>:<-p> through the login\n" +
			"node and hold it until Ctrl-C. --job <id> adopts an already-submitted job\n" +
			"instead of submitting. -I skips the tunnel entirely and opens an interactive\n" +
			"allocation (qsub -I / salloc) on a real tty.\n\n" +
			"    mu job tunnel -N hpc1 -s ~/serve.sh -p 8888\n" +
			"    mu job tunnel -N hpc1 --job 4501 -p 8888 -l 9999\n" +
			"    mu job tunnel -N hpc1 -I -q debug",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if interactive {
				return jobInteractive(node, account, queue_)
			}
			if script == "" && jobID == "" {
				return usageErr("script-mode tunnel needs -s <script> or --job <id> (or -I for a shell)")
			}
			if port == 0 {
				return usageErr("needs -p <port> — the service port on the compute node")
			}
			if localPort == 0 {
				localPort = port
			}
			return jobTunnel(node, script, jobID, account, queue_, port, localPort, yes, wait, poll)
		},
	}
	f := c.Flags()
	f.StringVarP(&node, "node", "N", "", "cluster to target (required off an HPC login node)")
	f.StringVarP(&script, "script", "s", "", "service script to submit, path resolved ON the target")
	f.StringVar(&jobID, "job", "", "adopt this already-submitted job instead of submitting")
	f.IntVarP(&port, "port", "p", 0, "service port on the compute node")
	f.IntVarP(&localPort, "local", "l", 0, "local port to listen on (default: same as --port)")
	f.StringVarP(&account, "account", "A", "", "allocation to charge (overrides the cluster's config default)")
	f.StringVarP(&queue_, "queue", "q", "", "queue / partition")
	f.BoolVarP(&interactive, "interactive", "I", false, "interactive allocation (qsub -I / salloc) instead of a tunnel")
	f.BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	f.DurationVar(&wait, "wait", 15*time.Minute, "give up if the job isn't running by then")
	f.DurationVar(&poll, "poll", 5*time.Second, "scheduler poll interval while waiting")
	c.MarkFlagsMutuallyExclusive("script", "job")
	c.MarkFlagsMutuallyExclusive("interactive", "script")
	c.MarkFlagsMutuallyExclusive("interactive", "job")
	c.MarkFlagsMutuallyExclusive("interactive", "port")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

// jobTunnel is the script-mode pipeline over ONE held connection: open an ssh
// ControlMaster (the single auth), submit and wait as channels on it, then add
// the port-forward to the same live connection and hold until Ctrl-C. The
// login node sees one session for the whole flow.
func jobTunnel(node, script, jobID, account, queue_ string, port, localPort int, yes bool, wait, poll time.Duration) error {
	if node == "" {
		return usageErr("needs -N <cluster> — the tunnel runs from the workstation")
	}
	scheduler := config.SchedulerFor(node)
	adapter := queue.For(scheduler)
	if adapter == nil {
		return errNoScheduler(node)
	}
	target, err := hpc.Resolve(node)
	if err != nil {
		return usageErr("%s", err)
	}
	if account == "" {
		account = config.AccountFor(node)
	}
	opts := queue.SubmitOpts{Account: account, Queue: queue_}
	submitCmd := adapter.SubmitCmd(script, opts)

	render.Info(fmt.Sprintf("Tunnel job → %s (%s)", node, scheduler))
	if jobID == "" {
		render.Detail("script:  " + script)
		render.Detail("command: " + submitCmd)
	} else {
		render.Detail("job:     " + jobID)
	}
	render.Detail(fmt.Sprintf("tunnel:  localhost:%d → <node>:%d, one held connection", localPort, port))
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

	mux, err := startMux(target)
	if err != nil {
		return runErr("%s: connect: %s", node, err)
	}
	defer mux.close()
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
	render.OK(fmt.Sprintf("tunnel up: http://localhost:%d → %s:%d (Ctrl-C to close)", localPort, host, port))
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
func startMux(target string) (*sshMux, error) {
	m := &sshMux{
		bin:    config.SSHCommand(),
		sock:   filepath.Join(os.TempDir(), fmt.Sprintf("mu-tun-%d", os.Getpid())),
		target: target,
		death:  make(chan error, 1),
	}
	m.master = exec.Command(m.bin, "-M", "-S", m.sock, "-N",
		"-o", "ConnectTimeout=10", target)
	m.master.Stdin, m.master.Stderr = os.Stdin, os.Stderr // Kerberos/host-key prompts stay answerable
	if err := m.master.Start(); err != nil {
		return nil, err
	}
	go func() { m.death <- m.master.Wait() }()
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
	cmd := exec.Command(m.bin, "-q", "-S", m.sock, m.target, "bash -lc "+shell.Quote(remoteCmd))
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
	return exec.Command(m.bin, "-q", "-S", m.sock, "-O", "forward",
		"-L", fmt.Sprintf("%d:%s:%d", localPort, host, port), m.target).Run()
}

// close asks the master to exit (which drops every channel and forward) and
// reaps it; the process kill is the belt for a wedged master.
func (m *sshMux) close() {
	_ = exec.Command(m.bin, "-q", "-S", m.sock, "-O", "exit", m.target).Run()
	if m.master.Process != nil {
		_ = m.master.Process.Kill()
	}
}

// jobInteractive replaces mu with `ssh -t <login> <qsub -I|salloc>` — the
// scheduler's own interactive allocation under a real tty (RemoteExec is
// tty-less by design, so this path builds its own ssh).
func jobInteractive(node, account, queue_ string) error {
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
	icmd := adapter.InteractiveCmd(queue.SubmitOpts{Account: account, Queue: queue_})
	render.Info(fmt.Sprintf("interactive allocation on %s: %s", label, icmd))
	ssh := config.SSHCommand()
	cmd := exec.Command(ssh, "-t", target, icmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil && !strings.Contains(err.Error(), "signal:") {
		return runErr("interactive session: %s", err)
	}
	return nil
}
