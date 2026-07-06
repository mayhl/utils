// Package proc is the local process-plane adapter: it lists processes via `ps`
// into a normalized []Process and signals a set of PIDs. Kept free of the CLI and
// render layers so the selector + kill primitive are unit-testable, and so the
// headless `mu ps kill` and a future interactive `mps -i` share one kill path.
package proc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Process is one local process, normalized across BSD (macOS) and GNU (Linux) ps.
type Process struct {
	PID     int
	PPID    int
	User    string
	State   string // OS-native code (R, S, S+, Ss…), preserved as-is
	Elapsed string // etime, e.g. "03:15:22" or "2-03:15:22"
	Name    string // argv0 basename — what a mask matches (pgrep-style)
	Command string // full command line
}

// psArgs picks the ps invocation per OS: macOS/BSD and Linux/GNU disagree on the
// all-processes flag and a couple of column keywords. Each emits, per line:
//
//	pid ppid user state etime command…   (command last; the only multi-word field)
func psArgs() []string {
	if runtime.GOOS == "darwin" {
		return []string{"-axo", "pid=,ppid=,user=,state=,etime=,command="}
	}
	return []string{"-eo", "pid=,ppid=,user=,stat=,etime=,args="}
}

// List runs ps and parses it into normalized processes, excluding our own PID so a
// mask never matches the killing process itself (mirrors pgrep excluding itself).
func List() ([]Process, error) {
	out, err := exec.Command("ps", psArgs()...).Output()
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var ps []Process
	for _, line := range strings.Split(string(out), "\n") {
		p, ok := parseLine(line)
		if !ok || p.PID == self {
			continue
		}
		ps = append(ps, p)
	}
	return ps, nil
}

// parseLine splits one ps line: the first five whitespace-separated fields are
// pid/ppid/user/state/etime (none contain spaces); the remainder is the command.
func parseLine(line string) (Process, bool) {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) < 6 {
		return Process{}, false
	}
	pid, err := strconv.Atoi(f[0])
	if err != nil {
		return Process{}, false
	}
	ppid, _ := strconv.Atoi(f[1])
	cmd := strings.Join(f[5:], " ") // Fields already collapsed runs of spaces
	return Process{
		PID:     pid,
		PPID:    ppid,
		User:    f[2],
		State:   f[3],
		Elapsed: f[4],
		Name:    filepath.Base(arg0(cmd)),
		Command: cmd,
	}, true
}

// arg0 is the first token of the command line (the executable path).
func arg0(cmd string) string {
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

// Signal sends sig to each pid, collecting per-pid failures into one error so a
// dead/renamed pid doesn't abort the rest of the set.
func Signal(pids []int, sig syscall.Signal) error {
	var failed []string
	for _, pid := range pids {
		if err := syscall.Kill(pid, sig); err != nil {
			failed = append(failed, strconv.Itoa(pid)+": "+err.Error())
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("signal failed for %s", strings.Join(failed, "; "))
	}
	return nil
}
