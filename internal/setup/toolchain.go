// Package setup holds the self-contained data the `mu setup` commands need — the
// dev-toolchain manifest embedded in the binary, so onboarding a fresh box needs no
// separate .config clone. Kept a leaf package (no cli/render deps) so both the command
// and its tests import it cheaply.
package setup

import (
	_ "embed"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

//go:embed toolchain.mise.toml
var manifestTOML string

type manifest struct {
	Tools map[string]string `toml:"tools"`
}

// Manifest returns the embedded toolchain manifest source (for --dump-manifest).
func Manifest() string { return manifestTOML }

// Specs parses the embedded manifest into mise install specs ("name@version"),
// sorted so the plan and install order are deterministic.
func Specs() ([]string, error) {
	var m manifest
	if err := toml.Unmarshal([]byte(manifestTOML), &m); err != nil {
		return nil, err
	}
	specs := make([]string, 0, len(m.Tools))
	for name, ver := range m.Tools {
		specs = append(specs, name+"@"+ver)
	}
	sort.Strings(specs)
	return specs, nil
}
