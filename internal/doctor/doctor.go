// Package doctor runs environment health checks for `mu doctor`: a few built-in,
// mu-intrinsic checks plus any executable "plugin" checks dropped in the checks
// dir (run-parts style). The plugin seam is how .config contributes dev-tooling
// checks (mise sole-owner, etc.) without mu depending on .config.
package doctor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
)

// Status is a check verdict; higher is worse (so max() finds the overall result).
type Status int

const (
	OK Status = iota
	Warn
	Fail
)

// Result is one check's outcome. Section groups results into tables (built-ins →
// "environment"; plugins → their checks.d subdir, or "checks" at top level).
// Verbose holds extra detail shown only under `-v` (a plugin's full output, etc.).
type Result struct {
	Section string
	Name    string
	Status  Status
	Detail  string
	Verbose string
}

type check func() Result

// Run executes the built-in checks then the checks.d plugins, in a stable order,
// returning all results and the worst status seen.
func Run() ([]Result, Status) {
	var results []Result
	for _, c := range builtins() {
		r := c()
		r.Section = "environment"
		results = append(results, r)
	}
	results = append(results, plugins()...)
	return results, worst(results)
}

func worst(rs []Result) Status {
	w := OK
	for _, r := range rs {
		if r.Status > w {
			w = r.Status
		}
	}
	return w
}

// isHPC reports whether we're on a login/compute node (ticket is inherited there,
// so its local check is skipped).
func isHPC() bool {
	return os.Getenv("BC_HOST") != "" || os.Getenv("MU_SYSTEM") == "hpc"
}

func builtins() []check {
	cs := []check{checkMise, checkConfig}
	if !isHPC() {
		cs = append(cs, checkTicket)
	}
	return cs
}

func checkMise() Result {
	if p, err := exec.LookPath("mise"); err == nil {
		return Result{Name: "mise", Status: OK, Detail: p}
	}
	if home, _ := os.UserHomeDir(); home != "" {
		if bin := filepath.Join(home, ".local", "bin", "mise"); isExec(bin) {
			return Result{Name: "mise", Status: Warn, Detail: bin + " (installed, not on PATH)"}
		}
	}
	return Result{Name: "mise", Status: Warn, Detail: "not found — dev toolchain unmanaged"}
}

func checkConfig() Result {
	defs := config.ClusterDefs()
	if len(defs) == 0 {
		return Result{Name: "hpc-config", Status: Warn, Detail: "no clusters configured (config.toml?)"}
	}
	var b strings.Builder
	for i, d := range defs {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "info\t%s\t%d node(s)", d.Name, len(d.Nodes)) // TSV → -v sub-table
	}
	return Result{Name: "hpc-config", Status: OK, Detail: fmt.Sprintf("%d cluster(s)", len(defs)), Verbose: b.String()}
}

func checkTicket() Result {
	info, ok := hpc.Ticket()
	switch {
	case !ok:
		return Result{Name: "ticket", Status: Warn, Detail: "klist not found"}
	case !info.Present:
		return Result{Name: "ticket", Status: Warn, Detail: "no Kerberos ticket — mu hpc ticket --renew"}
	case !info.Expires.IsZero() && time.Until(info.Expires) <= 0:
		return Result{Name: "ticket", Status: Warn, Detail: "expired — mu hpc ticket --renew"}
	default:
		detail := info.Principal // expiry folded into the row — scalar, no verbose block
		if !info.Expires.IsZero() {
			detail += " · expires " + info.Expires.Format("Jan 2 15:04")
		}
		return Result{Name: "ticket", Status: OK, Detail: detail}
	}
}

// checksDir is where plugin checks live (mu's data dir); MU_CHECKS_DIR overrides.
func checksDir() string {
	if d := os.Getenv("MU_CHECKS_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "mayhl_utils", "checks.d")
}

// plugins runs checks.d executables: top-level files (section "checks") plus one
// level of subdirs (section = dir name), so .config can group checks into module
// tables. exit 0 = OK, 2 = WARN, anything else = FAIL; last stdout line = detail.
func plugins() []Result {
	root := checksDir()
	out := runDir(root, "checks")
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		out = append(out, runDir(filepath.Join(root, d), d)...)
	}
	return out
}

func runDir(dir, section string) []Result {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if p := filepath.Join(dir, e.Name()); !e.IsDir() && isExec(p) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []Result
	for _, n := range names {
		out = append(out, runPlugin(dir, section, n))
	}
	return out
}

func runPlugin(dir, section, name string) Result {
	out, err := exec.Command(filepath.Join(dir, name)).Output()
	full := strings.TrimRight(string(out), "\n")
	detail := lastLine(full)
	verbose := ""
	if i := strings.LastIndex(full, "\n"); i >= 0 { // lines above the detail line → -v
		verbose = full[:i]
	}
	status := OK
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 2 {
			status = Warn
		} else {
			status = Fail
			if detail == "" {
				detail = err.Error()
			}
		}
	}
	return Result{Section: section, Name: name, Status: status, Detail: detail, Verbose: verbose}
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}
