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

// writeStaged pushes a local script's contents to stagedPath(id) on the cluster and returns that
// remote path. run executes one remote command under `bash -lc` (so $HOME resolves there); the
// content rides as a shell-quoted printf argument — the house idiom for writing a small remote
// file over a connection we already hold, rather than opening a second rsync/scp channel.
func writeStaged(run func(string) (string, error), localPath, id string) (string, error) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", err
	}
	file := fmt.Sprintf(`"$HOME/.local/state/mayhl_utils/jobs/%s.sh"`, id)
	cmd := fmt.Sprintf(`mkdir -p "$HOME/.local/state/mayhl_utils/jobs" && printf '%%s' %s > %s && chmod +x %s`,
		shell.Quote(string(data)), file, file)
	if _, err := run(cmd); err != nil {
		return "", fmt.Errorf("staging %s: %w", localPath, err)
	}
	return stagedPath(id), nil
}
