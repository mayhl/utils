package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The tunnel registry.
//
// A backgrounded tunnel outlives the mu that made it: the ssh master persists, the job keeps
// running, and the process that knew about both is gone. So the facts that make it
// closeable — which job, on which system, through which control socket, on which port — have
// to survive on disk. That file IS the tunnel, as far as `ls` and `close` are concerned.
//
// State, not cache: losing it doesn't cost a re-fetch, it strands a running job and a held
// port with no handle on either. Hence XDG_STATE_HOME, not XDG_CACHE_HOME.

// tunnelRec is one live tunnel.
type tunnelRec struct {
	System     string    `json:"system"`      // the cluster, as config names it
	Job        string    `json:"job"`         // scheduler job id
	Host       string    `json:"host"`        // compute node the job landed on
	Target     string    `json:"target"`      // ssh target of the login node
	Sock       string    `json:"sock"`        // the ssh ControlMaster socket
	LocalPort  int       `json:"local_port"`  // what you point a browser at
	RemotePort int       `json:"remote_port"` // what the service listens on, ON the node
	Walltime   string    `json:"walltime"`    // as requested; "" when the script decided
	Script     string    `json:"script"`      // "" in adopt mode
	Started    time.Time `json:"started"`
}

// URL is where the tunnel answers.
func (t tunnelRec) URL() string { return fmt.Sprintf("http://localhost:%d", t.LocalPort) }

// tunnelDir is where the registry lives — STATE, not cache: see the note above.
func tunnelDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "mayhl_utils", "tunnels")
}

// tunnelPath keys a tunnel by system+job: the pair is unique, and it's what you'd type.
func tunnelPath(system, job string) string {
	safe := strings.NewReplacer("/", "_", " ", "_").Replace(job)
	return filepath.Join(tunnelDir(), system+"-"+safe+".json")
}

// saveTunnel records a live tunnel. A registry mu couldn't write is worth failing over —
// unlike a cache, the alternative is a job and a port nobody can find again.
func saveTunnel(t tunnelRec) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	p := tunnelPath(t.System, t.Job)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// loadTunnels reads the registry, newest first. An unreadable entry is skipped, not fatal:
// one corrupt file must not hide the tunnels you can still close.
func loadTunnels() []tunnelRec {
	ents, err := os.ReadDir(tunnelDir())
	if err != nil {
		return nil
	}
	var out []tunnelRec
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tunnelDir(), e.Name()))
		if err != nil {
			continue
		}
		var t tunnelRec
		if json.Unmarshal(b, &t) == nil && t.Job != "" {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

// findTunnel resolves what the user typed — a job id, with or without its system — to one
// record. An ambiguous id (same job number on two systems) is an error, not a guess.
func findTunnel(ref string) (tunnelRec, error) {
	var hits []tunnelRec
	for _, t := range loadTunnels() {
		if t.Job == ref || strings.HasPrefix(t.Job, ref+".") || t.System+"/"+t.Job == ref {
			hits = append(hits, t)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return tunnelRec{}, usageErr("no tunnel %q — `mu job tunnel ls` shows the open ones", ref)
	default:
		return tunnelRec{}, usageErr("%q matches tunnels on several systems — qualify it as <system>/<job>", ref)
	}
}

func forgetTunnel(t tunnelRec) { _ = os.Remove(tunnelPath(t.System, t.Job)) }

// portFree reports whether a local port can be listened on. Binding is the only honest test
// — /proc and lsof both lie about what a bind will actually do.
func portFree(p int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// pickLocalPort chooses the local end of the forward.
//
// It starts at the REMOTE port, so `-p 8888` gives you localhost:8888 and the URL is the one
// you'd guess. Only when that's taken does it walk upward. A port the user NAMED (-l) is
// never silently moved: refusing is the honest answer, since quietly forwarding 3123 to 3124
// would leave them staring at whatever else holds 3123.
func pickLocalPort(want, remote int) (int, error) {
	if want != 0 {
		if want < 1024 {
			return 0, usageErr("local port %d is privileged (<1024) — pick one above 1024", want)
		}
		if !portFree(want) {
			return 0, usageErr("local port %d is already in use — pick another, or omit -l and mu will", want)
		}
		return want, nil
	}
	start := max(remote, 1024)
	for p := start; p < start+200 && p < 65535; p++ {
		if portFree(p) {
			return p, nil
		}
	}
	return 0, runErr("no free local port in %d..%d", start, start+200)
}
