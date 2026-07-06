package hpc

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/config"
)

// RemoteExec runs remoteCmd on target's login node over the transport ssh (MU_SSH),
// inside a login bash so /etc/profile.d puts the scheduler on PATH — `bash -lc`, the
// same reason the shell dispatcher uses bash not zsh. stdout is returned; the
// cluster's benign dbus/X11 login-profile noise is dropped from stderr
// (MU_SSH_STDERR_FILTER), the rest passed through so real errors still surface. The
// command is single-quoted so its own quotes/pipes reach the remote bash intact.
func RemoteExec(target, remoteCmd string) (string, error) {
	ssh := config.SSHCommand()
	arg := "bash -lc " + singleQuote(remoteCmd)
	cmd := exec.Command(ssh, "-q", target, arg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if s := filterStderr(stderr.String()); s != "" {
		fmt.Fprint(os.Stderr, s)
	}
	return stdout.String(), err
}

// singleQuote wraps s for a POSIX shell in single quotes, escaping embedded quotes.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
