package cli

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/render"
)

const probeTimeout = 2 * time.Second

func hpcCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "hpc",
		Short: "Cross-cluster HPC info (nodes/reachability; queue/storage later).",
		Long: "Aggregate info across the configured HPC clusters. Local-primary — run it from your\n" +
			"workstation to reach every cluster; on a login node you'll only see what's\n" +
			"reachable from there.",
	}
	c.AddCommand(hpcNodesCmd(), hpcTicketCmd())
	return c
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
