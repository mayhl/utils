// Package shell holds small helpers for building POSIX shell command strings safely.
package shell

import "strings"

// Quote wraps s for a POSIX shell in single quotes, escaping any embedded single quote —
// safe for remote-exec and interpolated args (e.g. PBS array brackets like "1284[7].hpc1"
// must not glob-expand; a value may hold spaces).
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
