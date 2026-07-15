package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// find walks the command tree by path (e.g. "hpc","nodes") and returns the leaf,
// failing if the path doesn't resolve to exactly that command. cobra's Find
// returns the deepest *matching* ancestor with the rest as args rather than an
// error, so a name check is what actually proves the subcommand is wired.
func find(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cmd, _, err := root.Find(path)
	if err != nil {
		t.Fatalf("Find(%v): %v", path, err)
	}
	if cmd.Name() != path[len(path)-1] {
		t.Fatalf("path %v resolved to %q — subcommand not wired", path, cmd.Name())
	}
	return cmd
}

// TestCommandTree pins the full set of wired command paths, so dropping or
// renaming a subcommand fails the build rather than silently vanishing from mu.
func TestCommandTree(t *testing.T) {
	root := Root()
	if root.Name() != "mu" {
		t.Errorf("root name = %q, want mu", root.Name())
	}
	for _, p := range [][]string{
		{"cp"},
		{"cp", "push"},
		{"cp", "pull"},
		{"sshfs"},
		{"sshfs", "list"},
		{"sshfs", "mount"},
		{"sshfs", "umount"},
		{"sshfs", "path"},
		{"sshfs", "add"},
		{"sshfs", "rm"},
		{"tar"},
		{"hpc"},
		{"hpc", "nodes"},
		{"hpc", "ticket"},
		{"shell-init"},
		{"log"},
		{"log", "write"},
		{"log", "clear"},
		{"doctor"},
	} {
		find(t, root, p...)
	}
}

// TestCommandFlags pins the user-facing flags (long name + shorthand) that scripts
// and muscle memory depend on, guarding against an accidental rename.
func TestCommandFlags(t *testing.T) {
	root := Root()
	for _, c := range []struct {
		path      []string
		long      string
		shorthand string // "" when the flag has no shorthand
	}{
		{[]string{"hpc", "nodes"}, "status", "s"},
		{[]string{"hpc", "ticket"}, "renew", ""},
		{[]string{"sshfs", "add"}, "ro", ""},
		{[]string{"sshfs", "add"}, "read-only", ""},
		{[]string{"tar"}, "gzip", "z"},
	} {
		cmd := find(t, root, c.path...)
		f := cmd.Flags().Lookup(c.long)
		if f == nil {
			t.Errorf("%v: missing --%s", c.path, c.long)
			continue
		}
		if c.shorthand != "" && f.Shorthand != c.shorthand {
			t.Errorf("%v: --%s shorthand = %q, want %q", c.path, c.long, f.Shorthand, c.shorthand)
		}
	}
	// -v / --quiet are global (persistent on root, inherited by every command). --quiet is
	// long-only on purpose: -q is --queue on the submit commands (see root.go).
	for _, g := range []struct{ long, shorthand string }{{"verbose", "v"}, {"quiet", ""}} {
		f := root.PersistentFlags().Lookup(g.long)
		if f == nil {
			t.Errorf("root missing persistent --%s", g.long)
			continue
		}
		if f.Shorthand != g.shorthand {
			t.Errorf("--%s shorthand = %q, want %q", g.long, f.Shorthand, g.shorthand)
		}
	}
}

// TestRootHelpRuns exercises the tree end-to-end: `mu --help` must execute without
// error (SilenceErrors/SilenceUsage are set) and list every top-level subcommand.
func TestRootHelpRuns(t *testing.T) {
	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mu --help: %v", err)
	}
	out := buf.String()
	for _, name := range []string{"cp", "sshfs", "tar", "hpc", "setup"} {
		if !strings.Contains(out, name) {
			t.Errorf("--help output missing %q:\n%s", name, out)
		}
	}
}

// TestSetupRelocation checks completion/shell-init moved under `setup` while the
// root-level aliases stay reachable (hidden) so existing rc lines don't break.
func TestSetupRelocation(t *testing.T) {
	root := Root()
	for _, sub := range []string{"completion", "shell-init"} {
		c, _, err := root.Find([]string{"setup", sub})
		if err != nil || c.Name() != sub {
			t.Errorf("mu setup %s missing: %v", sub, err)
		}
	}
	// The root shell-init alias still resolves but is hidden from help.
	c, _, err := root.Find([]string{"shell-init"})
	if err != nil || !c.Hidden {
		t.Errorf("hidden root shell-init alias missing or not hidden: %v (hidden=%v)", err, c != nil && c.Hidden)
	}
}
