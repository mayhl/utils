package cli

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/render"
)

// crashExitCode is the process code for an unhandled panic — distinct from the handled
// tiers (1 runtime, 2 usage; see exit.go) so a caller can tell "mu crashed" from "mu ran
// and reported a failure". 70 is sysexits EX_SOFTWARE (an internal software error).
const crashExitCode = 70

// Recover is main's last line of defense against an unhandled panic: deferred at the top
// of main, it turns a raw Go stack dump into a house error line. It persists the backtrace
// beside the event log (retrievable via `mu log`), prints a one-line house error pointing
// at the dump, tails the full stack inline only under MU_DEBUG, then exits crashExitCode.
// A nil recover (the normal return path) is a no-op. This deliberately owns a second
// os.Exit outside main — a recovered panic can't hand an error back to main for ExitCode.
func Recover() {
	r := recover()
	if r == nil {
		return
	}
	stack := debug.Stack()
	id, path := render.CrashDump(fmt.Sprintf("panic: %v", r), string(stack))
	hint := "set MU_DEBUG=1 for the backtrace"
	if path != "" {
		hint = "backtrace at " + path
	}
	render.Err(fmt.Sprintf("mu crashed: %v — %s [crash %s]", r, hint, id))
	if os.Getenv("MU_DEBUG") != "" {
		render.Detail(strings.TrimRight(string(stack), "\n")) // no trailing blank dim line
	}
	os.Exit(crashExitCode)
}
