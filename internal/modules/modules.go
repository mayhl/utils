// Package modules is mu's unified opt-in registry. MU_MODULES is a single env list
// (space- and/or comma-separated) naming the NEW modules and features a user has
// turned on — e.g. MU_MODULES='fmt git'. Core modules always register; only new
// modules gate on this. One list replaces a scatter of per-feature toggles (the
// `fmt` entry, for instance, replaces the old MU_MISE_FMT).
package modules

import (
	"os"
	"strings"
)

// Enabled reports whether name is listed in MU_MODULES. Case-insensitive; entries
// may be separated by spaces, tabs, or commas.
func Enabled(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	fields := strings.FieldsFunc(os.Getenv("MU_MODULES"), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	for _, f := range fields {
		if strings.ToLower(f) == name {
			return true
		}
	}
	return false
}
