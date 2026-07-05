// Package config is the single config layer for the mu engine. It reads a
// structured config.toml when present, falling back per-value to the inherited
// shell environment (the legacy config.env encoding) so nothing breaks during the
// migration. Platform seams (MU_SSH — ossh/ssh by mode) and terminal state
// (NO_COLOR/COLUMNS/…) are NOT config-file material and stay env-sourced.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// Cluster is one HPC cluster: its name, FQDN suffix, and node list.
type Cluster struct {
	Name   string
	Domain string
	Nodes  []string // sorted
}

// file is the config.toml schema. Clusters use an array-of-tables so their order
// is preserved as authored (a map would iterate randomly).
type file struct {
	HPCUser  string `toml:"hpc_user"`
	Transfer struct {
		RsyncOpts       string `toml:"rsync_opts"`
		SSHTransferOpts string `toml:"ssh_transfer_opts"`
	} `toml:"transfer"`
	SSHFS struct {
		Root string `toml:"root"`
	} `toml:"sshfs"`
	SSH struct {
		OSSH string `toml:"ossh"`
	} `toml:"ssh"`
	Clusters []struct {
		Name   string   `toml:"name"`
		Domain string   `toml:"domain"`
		Nodes  []string `toml:"nodes"`
	} `toml:"cluster"`
}

var (
	loaded   *file
	loadOnce sync.Once
)

// cfg returns the parsed config.toml, or nil if none is present/parseable (→
// callers fall back to the environment). Loaded once per process.
func cfg() *file {
	loadOnce.Do(load)
	return loaded
}

func load() {
	path := configPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var f file
	if err := toml.Unmarshal(data, &f); err != nil {
		fmt.Fprintf(os.Stderr, "mu: %s: %v (falling back to environment)\n", path, err)
		return
	}
	loaded = &f
}

// configPath resolves the config file: $MU_CONFIG_FILE, else $MU_ROOT/config.toml
// if it exists, else "" (no file → env fallback).
func configPath() string {
	if p := os.Getenv("MU_CONFIG_FILE"); p != "" {
		return p
	}
	if r := os.Getenv("MU_ROOT"); r != "" {
		p := filepath.Join(r, "config.toml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ClusterDefs yields the configured clusters, from config.toml when present else
// the MU_CLUSTERS + MU_CLUSTER_<UPPER>_{DOMAIN,NODES} env encoding. A cluster with
// no domain is skipped, matching the shell codegen and old Python resolver.
func ClusterDefs() []Cluster {
	if f := cfg(); f != nil && len(f.Clusters) > 0 {
		out := make([]Cluster, 0, len(f.Clusters))
		for _, c := range f.Clusters {
			if c.Domain == "" {
				continue
			}
			nodes := append([]string(nil), c.Nodes...)
			sort.Strings(nodes)
			out = append(out, Cluster{Name: c.Name, Domain: c.Domain, Nodes: nodes})
		}
		return out
	}
	return clusterDefsFromEnv()
}

func clusterDefsFromEnv() []Cluster {
	var out []Cluster
	for _, c := range strings.Fields(os.Getenv("MU_CLUSTERS")) {
		cu := strings.ToUpper(c)
		domain := os.Getenv("MU_CLUSTER_" + cu + "_DOMAIN")
		if domain == "" {
			continue
		}
		nodes := strings.Fields(os.Getenv("MU_CLUSTER_" + cu + "_NODES"))
		sort.Strings(nodes)
		out = append(out, Cluster{Name: c, Domain: domain, Nodes: nodes})
	}
	return out
}

// HPCUser is the raw HPC login name (config.toml hpc_user or MU_HPC_UNAME),
// possibly empty. Use for pkinit/targets; User() is the display form.
func HPCUser() string {
	if f := cfg(); f != nil && f.HPCUser != "" {
		return f.HPCUser
	}
	return os.Getenv("MU_HPC_UNAME")
}

// User is the HPC login name for display, or "?" when unset.
func User() string {
	if u := HPCUser(); u != "" {
		return u
	}
	return "?"
}

// RsyncOpts is the base transfer opts (config.toml, MU_HPC_RSYNC_OPTS, or default).
func RsyncOpts() string {
	if f := cfg(); f != nil && f.Transfer.RsyncOpts != "" {
		return f.Transfer.RsyncOpts
	}
	if v := os.Getenv("MU_HPC_RSYNC_OPTS"); v != "" {
		return v
	}
	return "-au --partial"
}

// SSHTransferOpts is the ssh options for transfers (config.toml, env, or default).
func SSHTransferOpts() string {
	if f := cfg(); f != nil && f.Transfer.SSHTransferOpts != "" {
		return f.Transfer.SSHTransferOpts
	}
	if v := os.Getenv("MU_SSH_TRANSFER_OPTS"); v != "" {
		return v
	}
	return "-q"
}

// SSHFSRoot is the local sshfs mount parent (config.toml, MU_SSHFS_ROOT, default).
// The ~ is left unexpanded here; callers expand as needed.
func SSHFSRoot() string {
	if f := cfg(); f != nil && f.SSHFS.Root != "" {
		return f.SSHFS.Root
	}
	if v := os.Getenv("MU_SSHFS_ROOT"); v != "" {
		return v
	}
	return "~/hpc_sshfs"
}

// OSSHPath is the path to the Kerberos `ossh` build (config.toml [ssh] ossh), or
// "" if unset. A machine-specific path, so it lives in config — the shell platform
// seam consumes it via MU_OSSH (exported by shell-init) to set MU_SSH, instead of
// hardcoding a path in the tracked toolkit.
func OSSHPath() string {
	if f := cfg(); f != nil {
		return f.SSH.OSSH
	}
	return ""
}

// SSHCommand is the transfer/transport ssh program. It's a platform SEAM (ossh on
// kerberized HPC access, ssh locally) set by the shell, so it stays env-sourced.
func SSHCommand() string {
	if s := os.Getenv("MU_SSH"); s != "" {
		return s
	}
	return "ssh"
}

// NodeTargets maps every configured node name to its user@node.domain target.
func NodeTargets() map[string]string {
	user := HPCUser()
	out := make(map[string]string)
	for _, c := range ClusterDefs() {
		for _, n := range c.Nodes {
			out[n] = user + "@" + n + "." + c.Domain
		}
	}
	return out
}

// NodeNames returns all configured node names across clusters, sorted.
func NodeNames() []string {
	var names []string
	for _, c := range ClusterDefs() {
		names = append(names, c.Nodes...)
	}
	sort.Strings(names)
	return names
}
