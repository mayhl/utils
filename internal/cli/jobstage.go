package cli

import (
	"fmt"
	"os"

	"github.com/mayhl/mayhl_utils/internal/shell"
)

// stageDir is where mu drops a PUSHED job script on the cluster: a per-user state dir on the
// shared filesystem — the compute node has to read it, so it can't live in /tmp on the login
// node — mirroring the local tunnel registry. Kept in ~ (tilde) form; the submit adapter and
// bash -lc both expand it on the far side.
const stageDir = "~/.local/state/mayhl_utils/jobs"

// stagedPath is the remote path a script with handle id lands at. Deterministic from the id, so
// the submit command can be built (and previewed) before the file is actually written.
func stagedPath(id string) string { return stageDir + "/" + id + ".sh" }

// isLocalScript reports whether the script argument names a file that exists on THIS machine —
// the signal that mu should PUSH it to the cluster rather than treat it as a path already there.
// An empty arg, a directory, or a name that only resolves on the cluster all read as "remote".
func isLocalScript(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// stageScriptCmd builds the remote command that writes a local script's contents to
// stagedPath(id) and marks it executable — a mkdir+printf+chmod chain run under `bash -lc` so
// $HOME resolves on the far side, the content riding as a shell-quoted printf argument (the
// house idiom for a small remote file, no second rsync/scp channel). Returned as a string, not
// executed, so the caller can run it on a connection it already holds OR chain it onto the
// submit with `&&` so staging and submit share ONE connection.
func stageScriptCmd(localPath, id string) (string, error) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", err
	}
	file := fmt.Sprintf(`"$HOME/.local/state/mayhl_utils/jobs/%s.sh"`, id)
	return fmt.Sprintf(`mkdir -p "$HOME/.local/state/mayhl_utils/jobs" && printf '%%s' %s > %s && chmod +x %s`,
		shell.Quote(string(data)), file, file), nil
}

// writeStaged pushes a local script to stagedPath(id) over run and returns that remote path —
// for callers that already hold a reused connection (the tunnel mux), where a separate write
// costs nothing. The sub path instead chains stageScriptCmd onto its submit, see job.go.
func writeStaged(run func(string) (string, error), localPath, id string) (string, error) {
	cmd, err := stageScriptCmd(localPath, id)
	if err != nil {
		return "", err
	}
	if _, err := run(cmd); err != nil {
		return "", fmt.Errorf("staging %s: %w", localPath, err)
	}
	return stagedPath(id), nil
}
