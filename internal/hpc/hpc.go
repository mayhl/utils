// Package hpc wraps the cluster inventory (internal/config) with the behaviors
// the transfer plane needs: node resolution, tab-completion, and Kerberos ticket
// acquisition. Mirrors the retired Python hpc.py; kept apart from config so the
// low-level env reader stays free of subprocess/auth side effects.
package hpc

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// Resolve accepts a bare node name (e.g. "hpc2") or an explicit "user@host"
// target. A bare name must be a configured node; otherwise an error listing the
// known nodes is returned.
func Resolve(nodeOrTarget string) (string, error) {
	if strings.Contains(nodeOrTarget, "@") {
		return nodeOrTarget, nil
	}
	if t, ok := config.NodeTargets()[nodeOrTarget]; ok {
		return t, nil
	}
	known := strings.Join(config.NodeNames(), ", ")
	if known == "" {
		known = "(none — is MU_CLUSTERS set?)"
	}
	return "", fmt.Errorf("unknown node: %s (known: %s)", nodeOrTarget, known)
}

// CompleteNode returns configured node names that start with prefix, for shell
// completion of the node argument.
func CompleteNode(prefix string) []string {
	var out []string
	for n := range config.NodeTargets() {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// NodesHint is a one-line "Known nodes: …" string for command help, read from
// the inherited env at construction time.
func NodesHint() string {
	names := config.NodeNames()
	if len(names) == 0 {
		return "No nodes configured (is MU_CLUSTERS set?)."
	}
	return "Known nodes: " + strings.Join(names, ", ") + "  ·  see 'mu hpc nodes' for targets."
}

// EnsureTicket obtains a Kerberos ticket if none is present. Called in the
// command body (not at construction) so --help and completion never trigger
// pkinit. It's a no-op on an HPC login/compute node ($BC_HOST set): the ticket is
// already there from login, so we never touch Kerberos there — mirroring the
// shell auth seam (mu_auth is `:` on hpc, pkinit on local). Also a no-op when
// MU_HPC_UNAME is unset or klist is absent.
func EnsureTicket() {
	if os.Getenv("BC_HOST") != "" || os.Getenv("MU_SYSTEM") == "hpc" {
		return
	}
	user := config.HPCUser()
	if user == "" {
		return
	}
	klist, err := exec.LookPath("klist")
	if err != nil {
		return
	}
	out, _ := exec.Command(klist).CombinedOutput()
	if strings.Contains(string(out), user) {
		return
	}
	render.Info(fmt.Sprintf("No Kerberos ticket for %s; running pkinit…", user))
	cmd := exec.Command("pkinit", user)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = cmd.Run()
}
