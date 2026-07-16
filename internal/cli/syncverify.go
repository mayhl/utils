package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/shell"
)

// remoteHashBatch bounds how many paths one sha256sum call takes, keeping the remote
// command line well under ARG_MAX; scientific pushes are usually a few large files, so
// this rarely chunks, but a many-small-file tier stays safe.
const remoteHashBatch = 200

// verifyPushed confirms, after a successful push, that each transferred file's bytes match
// on both ends — an independent end-to-end sha256, computed locally in Go and remotely with
// sha256sum. rsync already checksum-verifies every file it sends, so this is the opt-in
// (--verify) paranoid confirmation for a critical push, not a routine cost. Advisory: it
// reports a mismatch loudly but never fails the command — the transfer has already happened.
// It returns the local digests it computed, keyed by absolute local path, so the manifest
// can record them without hashing the same bytes a second time.
func verifyPushed(target, node string, results []syncResult, force bool) map[string]string {
	type bad struct{ path, reason string }
	var mismatches []bad
	var okCount int
	var totalBytes int64
	digests := map[string]string{}

	for _, res := range results {
		files := res.newPaths
		if force {
			files = append(append([]string{}, res.newPaths...), res.updates...)
		}
		if len(files) == 0 {
			continue
		}

		// Local digests in Go — no dependency on a local sha256 tool (macOS lacks
		// sha256sum by default).
		local := make(map[string]string, len(files))
		var want []string
		for _, rel := range files {
			h, sz, err := sha256File(filepath.Join(res.localAbs, rel))
			if err != nil {
				mismatches = append(mismatches, bad{rel, "local read failed: " + err.Error()})
				continue
			}
			local[rel] = h
			digests[filepath.Join(res.localAbs, rel)] = h
			totalBytes += sz
			want = append(want, rel)
		}

		remote := remoteSha256(target, res.dest, want)
		for _, rel := range want {
			rh, ok := remote[rel]
			switch {
			case !ok:
				mismatches = append(mismatches, bad{rel, "missing on remote"})
			case rh != local[rel]:
				mismatches = append(mismatches, bad{rel, "sha256 differs"})
			default:
				okCount++
			}
		}
	}

	total := okCount + len(mismatches)
	if total == 0 {
		return digests // nothing was pushed under these tiers — nothing to verify
	}
	if len(mismatches) == 0 {
		render.OK(fmt.Sprintf("verify:  %d/%d files match (sha256, %s)", okCount, total, render.HumanBytes(totalBytes)))
		return digests
	}
	render.Warn(fmt.Sprintf("verify:  %d/%d files match — %d MISMATCH on %s (the push may be corrupt or incomplete)", okCount, total, len(mismatches), node))
	const cap_ = 20
	for i, m := range mismatches {
		if i >= cap_ {
			render.Detail(fmt.Sprintf("  ... and %d more", len(mismatches)-cap_))
			break
		}
		render.Detail(fmt.Sprintf("  ✗ %s: %s", m.path, m.reason))
	}
	return digests
}

// sha256File streams a file through sha256, returning its hex digest and byte size.
func sha256File(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// remoteSha256 runs sha256sum on the pushed files under dest and returns a rel→digest map.
// It batches to bound the command line, and swallows sha256sum's per-file errors (a missing
// file just drops out of the map, surfacing as "missing on remote") so one absent file
// never discards the digests of the rest. A whole batch that fails to run leaves those
// paths unmapped — also reported as missing, the honest signal that the tier didn't land.
func remoteSha256(target, dest string, rels []string) map[string]string {
	out := make(map[string]string, len(rels))
	for i := 0; i < len(rels); i += remoteHashBatch {
		end := i + remoteHashBatch
		if end > len(rels) {
			end = len(rels)
		}
		var b strings.Builder
		for _, rel := range rels[i:end] {
			b.WriteString(" ")
			b.WriteString(shell.Quote(rel))
		}
		// `|| true` swallows the non-zero exit a missing file triggers; the good digests
		// still print on stdout, and the absent one is caught by its gap in the map.
		cmd := fmt.Sprintf("cd %s 2>/dev/null && sha256sum --%s 2>/dev/null || true", shell.Quote(dest), b.String())
		res, err := hpc.RemoteExec(target, cmd)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(res, "\n") {
			if h, p, ok := parseSha256Line(line); ok {
				out[p] = h
			}
		}
	}
	return out
}

// parseSha256Line splits a `sha256sum` output line — "<64-hex><2 sep chars><path>" — into
// its digest and path. The two separator chars are a space plus a mode flag (' ' text,
// '*' binary). ok=false for a short or malformed line (a stray warning, a blank).
func parseSha256Line(line string) (digest, path string, ok bool) {
	if len(line) < 67 || line[64] != ' ' {
		return "", "", false
	}
	digest = line[:64]
	if _, err := hex.DecodeString(digest); err != nil {
		return "", "", false
	}
	return digest, line[66:], true
}
