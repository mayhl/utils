package cli

import (
	"errors"
	"fmt"
)

// exitErr carries a house error message plus the process exit code it maps to. Leaf
// helpers and RunE funcs return it instead of calling os.Exit, so the single exit lives
// in main (via ExitCode) and the message renders once through HouseError. Codes: 2 =
// usage/config (bad flags, an unconfigured cluster, off-HPC with no --node), 1 = runtime
// (a remote/local command failed). A command may set another code via the struct directly for a
// distinct signal (dotfiles uses 4 for a .config reconcile conflict). A clean no-op is a nil
// error, not an exitErr.
type exitErr struct {
	code int
	msg  string
}

func (e *exitErr) Error() string { return e.msg }

// usageErr builds a code-2 error: a usage or configuration problem the user must fix
// before the command can run (bad flags, no scheduler configured, off-HPC with no --node).
func usageErr(format string, a ...any) error {
	return &exitErr{code: 2, msg: fmt.Sprintf(format, a...)}
}

// runErr builds a code-1 error: a runtime failure (a remote/local command errored) that
// isn't the user's input to fix.
func runErr(format string, a ...any) error {
	return &exitErr{code: 1, msg: fmt.Sprintf(format, a...)}
}

// ExitCode reports the process exit code for a command error: an exitErr's own code,
// else 1 for any other non-nil error. It matches only *exitErr, so a stray exec exit
// error can't leak a subprocess's code as mu's. main owns the single os.Exit.
func ExitCode(err error) int {
	var e *exitErr
	if errors.As(err, &e) {
		return e.code
	}
	return 1
}
