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
	"strings"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/shell"
)

// RemoteExec runs remoteCmd on target's login node over the transport ssh (MU_SSH),
// inside a login bash so /etc/profile.d puts the scheduler on PATH — `bash -lc`, the
// same reason the shell dispatcher uses bash not zsh. stdout is returned; the
// cluster's benign dbus/X11 login-profile noise is dropped from stderr
// (MU_SSH_STDERR_FILTER), the rest passed through so real errors still surface. The
// command is single-quoted so its own quotes/pipes reach the remote bash intact.
func RemoteExec(target, remoteCmd string) (string, error) {
	ssh := config.SSHCommand()
	arg := "bash -lc " + shell.Quote(remoteCmd)
	cmd := exec.Command(ssh, "-q", target, arg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if s := filterStderr(stderr.String()); s != "" {
		fmt.Fprint(os.Stderr, s)
	}
	if err != nil {
		return stdout.String(), errors.New(classify(target, err))
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
