package hpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/shell"
)

// RemoteExec runs remoteCmd on target's login node over the transport ssh (MU_SSH),
// inside a login bash so /etc/profile.d puts the scheduler on PATH — `bash -lc`, the
// same reason the shell dispatcher uses bash not zsh. stdout is returned; the
// cluster's benign dbus/X11 login-profile noise is dropped from stderr
// (MU_SSH_STDERR_FILTER), the rest passed through so real errors still surface. The
// command is single-quoted so its own quotes/pipes reach the remote bash intact.
// A bounded ConnectTimeout fails a dead host fast rather than hanging, and a latency
// spinner reassures once the call outlasts spinnerDelay.
func RemoteExec(target, remoteCmd string) (string, error) {
	ssh := config.SSHCommand()
	arg := "bash -lc " + shell.Quote(remoteCmd)
	cmd := exec.Command(ssh, "-q", "-o", fmt.Sprintf("ConnectTimeout=%d", connectSeconds()), target, arg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	stop := armSpinner(hostOf(target))
	err := cmd.Run()
	stop()
	if s := filterStderr(stderr.String()); s != "" {
		fmt.Fprint(os.Stderr, s)
	}
	if err != nil {
		return stdout.String(), errors.New(classify(target, err))
	}
	return stdout.String(), nil
}

// connectTimeout (seconds) bounds ssh's connect phase for the interactive
// single-host path, so a dead login node fails fast (via classify → "unreachable")
// instead of hanging on TCP defaults. Overridable via MU_SSH_CONNECT_TIMEOUT. It
// bounds only connection setup, not auth or the remote command's runtime, so a long
// qstat is never cut off. The fan-out path (RemoteExecTimeout) sets its own.
const connectTimeout = 10

func connectSeconds() int {
	if v := os.Getenv("MU_SSH_CONNECT_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return connectTimeout
}

// spinnerDelay is how long a remote call must run before the latency spinner
// appears — long enough that a fast LAN/cached call never flickers one, short
// enough to reassure on a slow login that mu isn't wedged.
const spinnerDelay = 400 * time.Millisecond

// armSpinner shows a "querying <host>" spinner if the caller hasn't stopped it
// within spinnerDelay, and returns a stop func that cancels the pending spinner
// (never shown) or clears it (shown). Single-host only — the concurrent fan-out
// would interleave frames. TTY-gating lives in render.Spinner (a no-op off-TTY).
func armSpinner(host string) func() {
	var (
		mu   sync.Mutex
		sp   *render.Spinner
		done bool
	)
	timer := time.AfterFunc(spinnerDelay, func() {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return
		}
		sp = render.NewSpinner("querying " + host + "…")
		sp.Start()
	})
	return func() {
		mu.Lock()
		defer mu.Unlock()
		done = true
		timer.Stop()
		if sp != nil {
			sp.Stop()
		}
	}
}

// LocalExec is the on-cluster counterpart to RemoteExec: it runs remoteCmd on the
// current login node with no ssh (mu is already on the box). Same login bash (`bash -lc`)
// as the remote arm so the scheduler command resolves identically — whether it's a PATH
// binary or a profile-defined wrapper — and the same benign login-profile noise is
// filtered from stderr. Two guarantees are inherited from RemoteExec by construction, not
// a shell pipe: only the interactive shell shows the login banner (a non-interactive
// `bash -lc` triggers no MOTD, the local mirror of the dispatcher's `ssh -q`), and stdout
// and stderr stay in SEPARATE buffers so login noise never contaminates the parsed stdout
// and the command's real exit code survives (a `2>&1 | grep` would lose both — the
// dispatcher avoids it with `2> >(grep …)` process substitution; here Go buffers do it).
// No reachability probe on the error path: there's no host to dial, so a failure is just
// its exit-code text.
func LocalExec(remoteCmd string) (string, error) {
	cmd := exec.Command("bash", "-lc", remoteCmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if s := filterStderr(stderr.String()); s != "" {
		fmt.Fprint(os.Stderr, s)
	}
	if err != nil {
		return stdout.String(), errors.New(exitText(err))
	}
	return stdout.String(), nil
}

// RemoteExecTimeout is RemoteExec bounded by a deadline — for concurrent
// cross-cluster fan-out where a wedged or unreachable host must never hang the
// whole collate. ssh ConnectTimeout fails an unreachable host fast; the context
// deadline kills anything still running past timeout. Unlike RemoteExec it does NOT
// print stderr (concurrent hosts would interleave) — a failure's first real stderr
// line is folded into the returned error, and a deadline hit returns a clean
// "timeout after …".
func RemoteExecTimeout(target, remoteCmd string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ssh := config.SSHCommand()
	arg := "bash -lc " + shell.Quote(remoteCmd)
	connTO := int(timeout.Seconds())
	if connTO < 1 {
		connTO = 1
	}
	cmd := exec.CommandContext(ctx, ssh, "-q", "-o", fmt.Sprintf("ConnectTimeout=%d", connTO), target, arg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return stdout.String(), fmt.Errorf("timeout after %s", timeout)
	}
	if err != nil {
		if msg := firstLine(filterStderr(stderr.String())); msg != "" {
			return stdout.String(), errors.New(msg)
		}
		return stdout.String(), errors.New(classify(target, err))
	}
	return stdout.String(), nil
}

// firstLine returns the first non-blank line of s, trimmed — a terse error summary.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}

// reachTimeout bounds the on-failure reachability probe.
const reachTimeout = 3 * time.Second

// classify turns a bare remote-exec failure (no remote stderr to show) into a human
// reason. It runs ONLY on the error path: a quick TCP dial of the login node's ssh
// port distinguishes an unreachable host (down/network) from a host that answered
// but couldn't log us in (auth/ticket). Assumes direct-to-login-node ssh (no proxy),
// so the dialed endpoint is the one ssh uses.
func classify(target string, err error) string {
	if !reachable(hostOf(target)) {
		return "unreachable (host down or network)"
	}
	return exitText(err)
}

// hostOf strips any "user@" from an ssh target, leaving the host to dial.
func hostOf(target string) string {
	if i := strings.LastIndex(target, "@"); i >= 0 {
		return target[i+1:]
	}
	return target
}

// reachable reports whether the host's ssh port accepts a TCP connection.
func reachable(host string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "22"), reachTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// exitText names a failure once the host is known reachable, so the cause is the
// login itself. ssh's catch-all 255 then means a Kerberos problem — a missing ticket
// (the common, fixable case, detected locally) or one the server rejected.
func exitText(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		switch ee.ExitCode() {
		case 255:
			if info, ok := Ticket(); ok && !info.Present {
				return "no Kerberos ticket — run `mu hpc ticket --renew`"
			}
			return "authentication failed — ticket expired or rejected (check `mu hpc ticket`)"
		case 127:
			return "scheduler command not found on PATH"
		case 126:
			return "permission denied"
		}
	}
	return err.Error()
}

// filterStderr drops the cluster's benign login-profile noise (default: the dbus/X11
// lines) so only real remote errors surface. Pattern overridable via
// MU_SSH_STDERR_FILTER, matching the shell dispatcher's grep filter.
func filterStderr(s string) string {
	if s == "" {
		return ""
	}
	pat := os.Getenv("MU_SSH_STDERR_FILTER")
	if pat == "" {
		pat = `dbus-update-activation-environment|^Cannot continue`
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return s
	}
	var keep []string
	for _, ln := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if strings.TrimSpace(ln) == "" || re.MatchString(ln) {
			continue
		}
		keep = append(keep, ln)
	}
	if len(keep) == 0 {
		return ""
	}
	return strings.Join(keep, "\n") + "\n"
}
