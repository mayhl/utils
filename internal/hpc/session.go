package hpc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/shell"
)

// Session is one held ssh connection (OpenSSH ControlMaster): mu owns the master process, and
// every leg — an exec, an rsync, a port-forward — rides it as a channel, so the whole flow
// costs ONE auth handshake and one session on the login node instead of one per command. The
// master is opened with -M -N (hold the connection, run no remote command); work then runs as
// separate `ssh -S <sock>` / `rsync -e 'ssh -S <sock>'` invocations against the socket.
//
// Extracted from the tunnel's original mux so exec, rsync, and forwarding share one
// connection-reuse mechanism rather than each re-authenticating — on a Kerberos cluster the
// auth, not the transfer, is the cost worth killing.
type Session struct {
	bin, sock, target string
	master            *exec.Cmd
	death             chan error // master exit (connection drop / teardown); unused when persisted
}

// SessionOpts tunes how the master is held.
type SessionOpts struct {
	// Persist detaches the master: ssh forks into the background (-f) and stays alive after mu
	// exits (ControlPersist), which is what makes a BACKGROUNDED tunnel possible — the forward
	// belongs to the master, so the master must outlive the process that asked for it. Default
	// false: a per-command master that dies when the caller Closes it (or when mu exits).
	Persist bool
	// ID names the control socket (mu-tun-<id>) when Persist is set, so a later mu can reattach
	// or close it; a pid that no longer exists is a poor handle for something you want to close
	// tomorrow. Ignored when not persisting (the socket is named for the pid).
	ID string
}

// OpenSession opens the master (auth prompts pass through) and waits until the control socket
// answers. The socket lives in the temp dir — short enough for the unix-socket path limit.
func OpenSession(target string, opts SessionOpts) (*Session, error) {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("mu-mux-%d", os.Getpid()))
	if opts.Persist {
		sock = filepath.Join(os.TempDir(), "mu-tun-"+opts.ID)
	}
	s := &Session{
		bin:    config.SSHCommand(),
		sock:   sock,
		target: target,
		death:  make(chan error, 1),
	}
	// A socket left by a crashed, force-killed, or older-mu master makes ssh refuse to open a
	// new one ("ControlSocket ... already exists, disabling multiplexing"). If nothing live
	// answers it, it's a corpse — reap it so a retry isn't wedged. Only the persist path names
	// the socket for the target (so it can collide); the pid-named path never does.
	if opts.Persist {
		if _, err := os.Stat(sock); err == nil {
			if exec.Command(s.bin, "-q", "-S", sock, "-O", "check", target).Run() != nil {
				_ = os.Remove(sock)
			}
		}
	}
	// -q: quiet the login banner the server prints on connect. -x: no X11 forwarding — a mux
	// never needs it, and a ~/.ssh/config that turns it on makes every channel print "No xauth
	// data; using fake authentication data", noise on a path whose whole output is a few lines.
	//
	// ServerAlive* keeps the master through an IDLE gap: a held tunnel can sit untouched for
	// hours while the job computes, and a NAT or an idle-timeout on the way will drop a silent
	// connection without telling either end — the master then lingers as a corpse whose forward
	// answers nothing. Probing every 30s (5 misses ≈ 2.5min to declare death) keeps the link
	// warm, and on a real drop the master EXITS instead of playing dead, so `ls` reads detached
	// and `reattach` can put it back. Cheap: the traffic is a keepalive packet a minute.
	args := []string{
		"-q", "-x", "-M", "-S", s.sock, "-N", "-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=5",
	}
	if opts.Persist {
		args = append(args, "-f", "-o", "ControlPersist=yes")
	}
	s.master = exec.Command(s.bin, append(args, target)...)
	s.master.Stdin, s.master.Stderr = os.Stdin, os.Stderr // Kerberos/host-key prompts stay answerable
	if err := s.master.Start(); err != nil {
		return nil, err
	}
	if opts.Persist {
		// -f means the process we started EXITS once the real master has forked away, so its
		// exit is success, not death. Nothing to watch: the socket check below is the test.
		_ = s.master.Wait()
		s.master = nil
	} else {
		go func() { s.death <- s.master.Wait() }()
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if exec.Command(s.bin, "-q", "-S", s.sock, "-O", "check", target).Run() == nil {
			return s, nil
		}
		select {
		case err := <-s.death:
			return nil, fmt.Errorf("connection failed: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			s.Close()
			return nil, fmt.Errorf("control socket not ready after 30s")
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// Run executes one remote command as a channel on the held connection — no new auth, no new
// session on the far side. Wrapped in `bash -lc` so $HOME/~ expand and the scheduler is on
// PATH, matching RemoteExec.
func (s *Session) Run(remoteCmd string) (string, error) {
	cmd := exec.Command(s.bin, "-q", "-x", "-S", s.sock, s.target, "bash -lc "+shell.Quote(remoteCmd))
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

// Forward adds -L localPort:host:port to the LIVE master — a tunnel joins the connection that
// has been open since submit.
func (s *Session) Forward(localPort int, host string, port int) error {
	return exec.Command(s.bin, "-q", "-x", "-S", s.sock, "-O", "forward",
		"-L", fmt.Sprintf("%d:%s:%d", localPort, host, port), s.target).Run()
}

// RsyncTransport is the -e value that makes rsync ride this session's master instead of dialing
// (and authenticating) a connection of its own: `ssh -x -S <sock> -o ControlMaster=no`.
// ControlMaster=no keeps rsync's ssh a CLIENT of the existing master, never a second master.
func (s *Session) RsyncTransport() string {
	return fmt.Sprintf("%s -x -S %s -o ControlMaster=no", s.bin, s.sock)
}

// Sock is the control-socket path — the handle the tunnel registry stores so a later mu can
// close a persisted master it never started.
func (s *Session) Sock() string { return s.sock }

// Death signals the master's exit (a dropped connection or teardown) for a foreground caller to
// select on. A persisted master forks away and is not watched, so this never fires there.
func (s *Session) Death() <-chan error { return s.death }

// Close asks the master to exit (dropping every channel and forward) and reaps it; the process
// kill is the belt for a wedged master.
func (s *Session) Close() {
	_ = exec.Command(s.bin, "-q", "-S", s.sock, "-O", "exit", s.target).Run()
	// The persist path forks the real master away and nils master, so `-O exit` above is what
	// actually tears it down; the Kill is only the belt for a non-persist (foreground) master.
	if s.master != nil && s.master.Process != nil {
		_ = s.master.Process.Kill()
	}
}
