package shellinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayhl/mayhl_utils/internal/config"
)

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
hpc_user = "alice"
[[cluster]]
name = "alpha"
domain = "alpha.example.mil"
nodes = ["hpc2", "hpc1"]
[[cluster]]
name = "beta"
domain = "beta.example.mil"
nodes = ["node2"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MU_CONFIG_FILE", path)
	t.Setenv("MU_NODE", "hpc1") // this shell is "on" hpc1 → its dispatcher is skipped
	config.ResetForTest()       // config memoizes per-process; reload from this file

	out := Generate()

	// Config exports (the bridge that lets config.env be retired).
	for _, want := range []string{
		`export MU_HPC_UNAME="alice"`,
		`export MU_CLUSTERS="alpha beta"`,
		`export MU_CLUSTER_ALPHA_DOMAIN="alpha.example.mil"`,
		`export MU_CLUSTER_ALPHA_NODES="hpc1 hpc2"`, // nodes sorted
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing export %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "mu_node() {") {
		t.Error("missing shared dispatcher helper")
	}
	// The dispatcher grammar (help arm, numbered-node selector) is verified
	// behaviorally by TestDispatchExec, which runs the generated code — a text
	// match here would just duplicate that, more brittly.
	if !strings.Contains(out, `hpc2() { mu_node hpc2 "alice@hpc2.alpha.example.mil" "$@"; }`) {
		t.Errorf("missing/wrong hpc2 wrapper:\n%s", out)
	}
	if !strings.Contains(out, `node2() { mu_node node2 "alice@node2.beta.example.mil" "$@"; }`) {
		t.Errorf("missing node2 wrapper:\n%s", out)
	}
	if strings.Contains(out, "hpc1()") {
		t.Error("self node (hpc1) should be skipped")
	}
	// Front-doors: mps/mkill always; the queue pair under the default (pbs) idiom.
	for _, want := range []string{
		`mps() { mu ps "$@"; }`,
		`mkill() { mu ps kill "$@"; }`,
		`mstat() { mu hpc queue "$@"; }`,
		`mdel() { mu hpc queue kill "$@"; }`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing front-door %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "mqueue()") {
		t.Error("pbs idiom should not emit mqueue")
	}
}

// TestFrontDoorIdiom pins the [shell] queue_aliases switch: slurm REPLACES the queue
// pair with mqueue/mcancel, both emits all four, and mps/mkill are idiom-independent.
func TestFrontDoorIdiom(t *testing.T) {
	cases := map[string]struct {
		want    []string
		notWant []string
	}{
		"slurm": {
			want:    []string{`mqueue() { mu hpc queue "$@"; }`, `mcancel() { mu hpc queue kill "$@"; }`},
			notWant: []string{"mstat()", "mdel()"},
		},
		"both": {
			want: []string{"mstat()", "mdel()", "mqueue()", "mcancel()"},
		},
	}
	for idiom, tc := range cases {
		t.Run(idiom, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			body := `
hpc_user = "alice"
[shell]
queue_aliases = "` + idiom + `"
[[cluster]]
name = "alpha"
domain = "alpha.example.mil"
nodes = ["hpc1"]
`
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MU_CONFIG_FILE", path)
			t.Setenv("MU_NODE", "none")
			config.ResetForTest()

			out := Generate()
			// mps/mkill are always present, whatever the idiom.
			if !strings.Contains(out, `mps() { mu ps "$@"; }`) {
				t.Errorf("mps missing under %q idiom:\n%s", idiom, out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("%q idiom missing %q in:\n%s", idiom, w, out)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(out, nw) {
					t.Errorf("%q idiom should not emit %q in:\n%s", idiom, nw, out)
				}
			}
		})
	}
}
