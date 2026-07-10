// Package config is the single config layer for the mu engine. It reads a
// structured config.toml; when a value is absent it falls back to a built-in
// default. config.toml is the sole source — the legacy config.env / MU_* env
// encoding is retired (shell consumers get the values from `mu shell-init`, which
// re-exports config.toml). Platform seams (MU_SSH — ossh/ssh by mode) and terminal
// state (NO_COLOR/COLUMNS/…) are NOT config-file material and stay env-sourced.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// Cluster is one HPC cluster: its name, FQDN suffix, node list, scheduler, and whether
// it's in the active set (collate targets active clusters by default).
type Cluster struct {
	Name         string
	Domain       string
	Nodes        []string          // sorted
	Scheduler    string            // "pbs" | "slurm"; "" if unset
	Active       bool              // in the default collate set (config `active`, default true)
	Account      string            // default allocation to charge on submit; "" if unset (mu job sub -A overrides)
	CoresPerNode int               // cores per compute node → MaxNodes = ceil(MaxCores / this); 0 = unset
	QueueClass   map[string]string // queue name → forced node class, overriding the name heuristic
	QueueCores   map[string]int    // queue name → cores/node override (GPU/specialty nodes)
}

// MirrorSet is one mirror-set: roots sharing a relative-path namespace, mapped
// under $ARCHIVE_HOME/<archive_rel> by the archive projection. Root ARITY encodes
// swap-ability: 2 roots = a [permanent, scratch] swap pair (that order), 1 root =
// archive-only. Sets may nest under the default $HOME/$WORKDIR pair (e.g. a group
// share at ~/projects/shared) — longest-prefix match picks the winner.
type MirrorSet struct {
	Name       string   `toml:"name"`
	Roots      []string `toml:"roots"`
	ArchiveRel string   `toml:"archive_rel"`
}

// file is the config.toml schema. Clusters use an array-of-tables so their order
// is preserved as authored (a map would iterate randomly).
type file struct {
	HPCUser  string   `toml:"hpc_user"`
	Fleet    []string `toml:"fleet"` // node names --fleet queries (one fetch each); empty → fall back to active clusters
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
	Shell struct {
		QueueAliases string `toml:"queue_aliases"` // idiom for the queue front-door names: "pbs"|"slurm"|"both"
	} `toml:"shell"`
	Project struct {
		CaseGlob           string `toml:"case_glob"`            // case-dir basename glob; default "case_*"
		DataDir            string `toml:"data_dir"`             // shared-data rel path in a project; default "simulations/data"
		TarParentThreshold string `toml:"tar_parent_threshold"` // batch-put cutoff, human size; default "1GB"
		TarHookThreshold   string `toml:"tar_hook_threshold"`   // per-leaf tar size warranting a pack hook; default "100GB"
		JobHooks           *bool  `toml:"job_hooks"`            // read-time model hooks in queue views; nil (omitted) → on
	} `toml:"project"`
	MirrorSets []MirrorSet `toml:"mirror_set"`
	Clusters   []struct {
		Name         string            `toml:"name"`
		Domain       string            `toml:"domain"`
		Nodes        []string          `toml:"nodes"`
		Scheduler    string            `toml:"scheduler"`
		Active       *bool             `toml:"active"`         // nil (omitted) → active by default
		Account      string            `toml:"account"`        // default submit allocation; optional
		CoresPerNode int               `toml:"cores_per_node"` // cores per node → MaxNodes; optional
		QueueClass   map[string]string `toml:"queue_class"`    // queue → forced node class; optional
		QueueCores   map[string]int    `toml:"queue_cores"`    // queue → cores/node override; optional
	} `toml:"cluster"`
}

var (
	loaded   *file
	loadOnce sync.Once
)

// cfg returns the parsed config.toml, or nil if none is present/parseable (→
// callers use built-in defaults). Loaded once per process.
func cfg() *file {
	loadOnce.Do(load)
	return loaded
}

// ResetForTest clears the memoized config so a test can repoint MU_CONFIG_FILE at
// a fresh file and reload. Test-only: config is otherwise loaded once per process.
func ResetForTest() {
	loaded = nil
	loadOnce = sync.Once{}
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
		fmt.Fprintf(os.Stderr, "mu: %s: %v (using built-in defaults)\n", path, err)
		return
	}
	loaded = &f
}

// configPath resolves the config file: $MU_CONFIG_FILE, else $MU_ROOT/config.toml
// if it exists, else "" (no file → built-in defaults, empty cluster list).
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

// Path returns the resolved config.toml path ($MU_CONFIG_FILE, else $MU_ROOT/config.toml
// when it exists), or "" when none is set. Exported for `mu setup sync`, which reads and
// propagates the raw file rather than the parsed struct (to preserve comments).
func Path() string { return configPath() }

// ClusterDefs yields the configured clusters from config.toml, order preserved. A
// cluster with no domain is skipped, matching the shell codegen and old resolver.
func ClusterDefs() []Cluster {
	f := cfg()
	if f == nil {
		return nil
	}
	out := make([]Cluster, 0, len(f.Clusters))
	for _, c := range f.Clusters {
		if c.Domain == "" {
			continue
		}
		nodes := append([]string(nil), c.Nodes...)
		sort.Strings(nodes)
		out = append(out, Cluster{
			Name: c.Name, Domain: c.Domain, Nodes: nodes,
			Scheduler:    strings.ToLower(c.Scheduler),
			Active:       c.Active == nil || *c.Active, // default true
			Account:      c.Account,
			CoresPerNode: c.CoresPerNode,
			QueueClass:   c.QueueClass,
			QueueCores:   c.QueueCores,
		})
	}
	return out
}

// SchedulerFor returns the configured scheduler ("pbs"|"slurm") for the cluster
// owning node n, or "" if the node or its scheduler is unknown. Used by the live
// fetch to pick qstat vs squeue.
func SchedulerFor(node string) string {
	for _, c := range ClusterDefs() {
		for _, n := range c.Nodes {
			if n == node {
				return c.Scheduler
			}
		}
	}
	return ""
}

// AccountFor returns the configured default submit allocation for the cluster owning
// node n, or "" if unset. `mu job sub -A` overrides it; empty falls through to the
// scheduler / script default.
func AccountFor(node string) string {
	for _, c := range ClusterDefs() {
		if c.Name == node {
			return c.Account
		}
		for _, n := range c.Nodes {
			if n == node {
				return c.Account
			}
		}
	}
	return ""
}

// clusterFor returns the cluster owning node n — matched by cluster name (the `mu hpc
// queues` label is a cluster) or by a node in its list — and whether one matched.
func clusterFor(node string) (Cluster, bool) {
	for _, c := range ClusterDefs() {
		if c.Name == node {
			return c, true
		}
		for _, n := range c.Nodes {
			if n == node {
				return c, true
			}
		}
	}
	return Cluster{}, false
}

// CoresPerNodeFor returns the cluster's default cores-per-node (for the MaxNodes column),
// or 0 if unset — callers render "--" when it's 0.
func CoresPerNodeFor(node string) int {
	c, _ := clusterFor(node)
	return c.CoresPerNode
}

// QueueClassOverride returns the config-forced node class for a specific queue on the
// cluster owning node, or "" when there's no override (caller falls back to the name
// heuristic).
func QueueClassOverride(node, queue string) string {
	c, _ := clusterFor(node)
	return c.QueueClass[queue]
}

// QueueCoresOverride returns the per-queue cores/node override, or 0 when there's none
// (caller falls back to CoresPerNodeFor) — for GPU/specialty queues whose node core count
// differs from the cluster default.
func QueueCoresOverride(node, queue string) int {
	c, _ := clusterFor(node)
	return c.QueueCores[queue]
}

// Fleet is the explicit list of node names `--fleet` queries — one fetch per node, so a
// DSRC whose nodes are separate schedulers (each its own queue) is collated in full rather
// than collapsed to one representative node. Empty when unset → callers fall back to
// one-node-per-active-cluster.
func Fleet() []string {
	if f := cfg(); f != nil {
		return f.Fleet
	}
	return nil
}

// ActiveClusters is the subset flagged active (default true) — the set bare-mstat /
// collate targets by default, vs an explicit --all that includes inactive clusters.
func ActiveClusters() []Cluster {
	var out []Cluster
	for _, c := range ClusterDefs() {
		if c.Active {
			out = append(out, c)
		}
	}
	return out
}

// HPCUser is the raw HPC login name (config.toml hpc_user), possibly empty. Use
// for pkinit/targets; User() is the display form.
func HPCUser() string {
	if f := cfg(); f != nil {
		return f.HPCUser
	}
	return ""
}

// User is the HPC login name for display, or "?" when unset.
func User() string {
	if u := HPCUser(); u != "" {
		return u
	}
	return "?"
}

// RsyncOpts is the base transfer opts (config.toml [transfer] rsync_opts, or the
// built-in default). No -v/-P/--progress — the tool owns progress rendering.
func RsyncOpts() string {
	if f := cfg(); f != nil && f.Transfer.RsyncOpts != "" {
		return f.Transfer.RsyncOpts
	}
	return "-au --partial"
}

// SSHTransferOpts is the ssh options for transfers (config.toml, or default).
func SSHTransferOpts() string {
	if f := cfg(); f != nil && f.Transfer.SSHTransferOpts != "" {
		return f.Transfer.SSHTransferOpts
	}
	return "-q"
}

// SSHFSRoot is the local sshfs mount parent (config.toml [sshfs] root, or the
// default). The ~ is left unexpanded here; callers expand as needed.
func SSHFSRoot() string {
	if f := cfg(); f != nil && f.SSHFS.Root != "" {
		return f.SSHFS.Root
	}
	return "~/hpc_sshfs"
}

// CaseGlob is the case-dir basename glob the resolvers classify by (config.toml
// [project] case_glob). The blessed layout's convention is the default.
func CaseGlob() string {
	if f := cfg(); f != nil && f.Project.CaseGlob != "" {
		return f.Project.CaseGlob
	}
	return "case_*"
}

// ProjectDataDir is the shared-data rel path inside a project tree (config.toml
// [project] data_dir) — the tier that archives from $WORK and is add-only.
func ProjectDataDir() string {
	if f := cfg(); f != nil && f.Project.DataDir != "" {
		return f.Project.DataDir
	}
	return "simulations/data"
}

// JobHooks reports whether queue views fetch read-time model-hook data
// (config.toml [project] job_hooks) — default ON when the project module is
// enabled; the off-switch exists because the fetch adds a remote exec.
func JobHooks() bool {
	if f := cfg(); f != nil && f.Project.JobHooks != nil {
		return *f.Project.JobHooks
	}
	return true
}

// TarParentThreshold is the batch-put cutoff (config.toml [project]
// tar_parent_threshold, e.g. "500MB"): `archive put <parent>` packs ONE
// parent-level tar when every case leaf is under it, per-leaf tars otherwise.
func TarParentThreshold() int64 {
	if f := cfg(); f != nil {
		if n, ok := parseSize(f.Project.TarParentThreshold); ok {
			return n
		}
	}
	return 1 << 30
}

// TarHookThreshold flags a case leaf too big for one tar (config.toml [project]
// tar_hook_threshold): past it `archive put` still packs a single per-leaf tar
// but warns that a model pack hook should split it. FUTURE: run the hook.
func TarHookThreshold() int64 {
	if f := cfg(); f != nil {
		if n, ok := parseSize(f.Project.TarHookThreshold); ok {
			return n
		}
	}
	return 100 << 30
}

// parseSize reads a human size in the house units (B/KB/MB/GB/TB — HumanBytes's
// names, binary multiples): "500MB", "1.5GB". ok=false on empty or garbage, so
// the accessors fall back to their defaults.
func parseSize(s string) (int64, bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for i, u := range []string{"KB", "MB", "GB", "TB"} {
		if strings.HasSuffix(s, u) {
			mult = 1 << (10 * (i + 1))
			s = strings.TrimSuffix(s, u)
			break
		}
	}
	if mult == 1 {
		s = strings.TrimSuffix(s, "B")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return int64(f * float64(mult)), true
}

// MirrorSets are the extra configured mirror sets ([[mirror_set]]); the default
// $HOME/$WORKDIR/$ARCHIVE_HOME set is env-derived by the mirror package, not config.
func MirrorSets() []MirrorSet {
	if f := cfg(); f != nil {
		return f.MirrorSets
	}
	return nil
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

// QueueAliases is the scheduler idiom for the queue front-door names (config.toml
// [shell] queue_aliases): "pbs" (default) → mstat/mdel, "slurm" → mqueue/mcancel,
// "both" → all four. Pure ergonomics — which word your fingers reach for; the engine
// auto-detects each cluster's real scheduler regardless. "q" is a synonym for "both";
// an unset/unrecognized value falls back to "pbs".
func QueueAliases() string {
	if f := cfg(); f != nil {
		switch strings.ToLower(strings.TrimSpace(f.Shell.QueueAliases)) {
		case "slurm":
			return "slurm"
		case "both", "q":
			return "both"
		}
	}
	return "pbs"
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
