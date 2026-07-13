package cli

import "testing"

// TestTransferDst covers the optional-dst rule: an explicit 3rd arg wins, and with only
// <node> <src> each side falls back to its own default — "" for a push (rsync reads a
// bare `node:` as the remote home) and "." for a pull.
func TestTransferDst(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		def  string
		want string
	}{
		{"push explicit", []string{"hpc1", "run", "/p/work/run"}, "", "/p/work/run"},
		{"push default home", []string{"hpc1", "run"}, "", ""},
		{"pull explicit", []string{"hpc1", "out", "./local"}, ".", "./local"},
		{"pull default cwd", []string{"hpc1", "out"}, ".", "."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := transferDst(tc.args, tc.def); got != tc.want {
				t.Errorf("transferDst(%q, %q) = %q, want %q", tc.args, tc.def, got, tc.want)
			}
		})
	}
}
