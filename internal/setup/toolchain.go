// Package setup holds the self-contained data the `mu setup` commands need — the
// dev-toolchain manifest embedded in the binary, so onboarding a fresh box needs no
// separate .config clone. Kept a leaf package (no cli/render deps) so both the command
// and its tests import it cheaply.
package setup

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

//go:embed toolchain.mise.toml
var manifestTOML string

type manifest struct {
	Tools map[string]any `toml:"tools"`
}

// Manifest returns the embedded toolchain manifest source (for --dump-manifest).
func Manifest() string { return manifestTOML }

// Specs parses the embedded manifest into mise install specs. A bare string value is
// "name@version"; an inline table carries its tool options in mise's CLI form,
// "name[k=v,…]@version" (e.g. matching=musl), so the bare per-user install keeps them.
// Sorted so the plan and install order are deterministic.
func Specs() ([]string, error) {
	var m manifest
	if err := toml.Unmarshal([]byte(manifestTOML), &m); err != nil {
		return nil, err
	}
	specs := make([]string, 0, len(m.Tools))
	for name, v := range m.Tools {
		switch t := v.(type) {
		case string:
			specs = append(specs, name+"@"+t)
		case map[string]any:
			ver, _ := t["version"].(string)
			if ver == "" {
				return nil, fmt.Errorf("tool %s: inline table needs a version", name)
			}
			opts := make([]string, 0, len(t)-1)
			for k, ov := range t {
				if k != "version" {
					opts = append(opts, fmt.Sprintf("%s=%v", k, ov))
				}
			}
			sort.Strings(opts)
			spec := name
			if len(opts) > 0 {
				spec += "[" + strings.Join(opts, ",") + "]"
			}
			specs = append(specs, spec+"@"+ver)
		default:
			return nil, fmt.Errorf("tool %s: unsupported value %T", name, v)
		}
	}
	sort.Strings(specs)
	return specs, nil
}
