// Package rsync builds rsync argument vectors and runs transfers with a live
// progress bar. It ports the retired Python cp implementation: the tool owns the
// progress/verbose flags (so its --info=progress2 parser can't be corrupted by a
// user-supplied -v/-P/--progress), layers options env → named flags → --ropt with
// rightmost-wins, and warns on cross-layer duplicates.
package rsync

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// Opts are the named transfer flags exposed on `mu cp push/pull`.
type Opts struct {
	DryRun     bool
	Exclude    []string
	Delete     bool
	Bwlimit    string
	Compress   bool
	Checksum   bool
	Timeout    int
	PartialDir bool
	Ropt       []string
}

// partialDir keeps rsync's cross-run partials out of the destination tree.
const partialDir = ".rsync-partial"

// rsync --info=progress2 line, e.g. "  1,234,567  45%  12.34MB/s    0:00:12".
var progressRE = regexp.MustCompile(`([\d,]+)\s+(\d+)%\s+(\S+/s)\s+(\d+:\d\d:\d\d)`)

// Progress/verbose flags the tool owns; stripped from user layers so they can't
// corrupt the --info=progress2 parser. Long forms drop whole; short clusters
// keep their other letters (-avuP → -au).
var progressLong = map[string]bool{"--verbose": true, "--progress": true}

// Canonical option keys for cross-layer duplicate detection. --exclude is
// intentionally absent — multiple excludes stack, they don't conflict.
var (
	shortKey = map[rune]string{'z': "--compress", 'c': "--checksum"}
	longKey  = map[string]string{
		"--compress": "--compress", "--checksum": "--checksum",
		"--timeout": "--timeout", "--partial-dir": "--partial-dir",
		"--delete": "--delete", "--bwlimit": "--bwlimit",
	}
)

// BuildArgs assembles the rsync argument vector (without the leading
// "--info=progress2"/"-vP", which Run prepends). Layer order, later wins: env
// base (MU_HPC_RSYNC_OPTS) → named flags → --ropt, then the ssh transport and
// src/dst.
func BuildArgs(src, dst string, o Opts) []string {
	transport := strings.TrimSpace(config.SSHCommand() + " " + config.SSHTransferOpts())

	env := sanitize(shellSplit(config.RsyncOpts()), "MU_HPC_RSYNC_OPTS")

	// --ropt: progress-sanitize, then drop exact repeats.
	var ropt []string
	seen := map[string]bool{}
	for _, opt := range sanitize(o.Ropt, "--ropt") {
		if seen[opt] {
			render.Warn(fmt.Sprintf("duplicate --ropt '%s' ignored", opt))
			continue
		}
		seen[opt] = true
		ropt = append(ropt, opt)
	}

	// Warn when a named flag repeats an option already set in a raw layer.
	rawKeys := canonKeys(env)
	for k := range canonKeys(ropt) {
		rawKeys[k] = true
	}
	var named []string
	flag := func(active bool, tokens []string, key string) {
		if !active {
			return
		}
		if rawKeys[key] {
			render.Warn(fmt.Sprintf("%s set via both a flag and a raw opt; rightmost wins (--ropt > flag > env)", key))
		}
		named = append(named, tokens...)
	}
	flag(o.Compress, []string{"-z"}, "--compress")
	flag(o.Checksum, []string{"-c"}, "--checksum")
	flag(o.Delete, []string{"--delete"}, "--delete")
	flag(o.Bwlimit != "", []string{"--bwlimit", o.Bwlimit}, "--bwlimit")
	flag(o.Timeout > 0, []string{"--timeout", strconv.Itoa(o.Timeout)}, "--timeout")
	flag(o.PartialDir, []string{"--partial-dir=" + partialDir}, "--partial-dir")
	for _, ex := range o.Exclude {
		named = append(named, "--exclude", ex)
	}
	if o.DryRun {
		named = append(named, "--dry-run")
	}

	args := make([]string, 0, len(env)+len(named)+len(ropt)+4)
	args = append(args, env...)
	args = append(args, named...)
	args = append(args, ropt...)
	args = append(args, "-e", transport, src, dst)
	return args
}

// Run executes rsync with the given args and returns its exit code. The default
// mode shows the aggregate progress bar; verbose streams raw per-file rsync
// (-vP) straight to the terminal, which can't fold into an aggregate bar. In
// both modes stderr stays attached so ssh host-key prompts and errors surface.
func Run(args []string, label string, verbose bool) int {
	if verbose {
		cmd := exec.Command("rsync", append([]string{"-vP"}, args...)...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return exitCode(cmd.Run())
	}
	return runProgress(args, label)
}

func runProgress(args []string, label string) int {
	// -v surfaces each filename (→ the bar's live label); --stats yields the
	// end-of-run block we parse into a house summary. Both ride the same stdout
	// pipe as the progress2 aggregate line.
	cmd := exec.Command("rsync", append([]string{"--info=progress2", "-v", "--stats"}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		render.Err(err.Error())
		return 1
	}

	bar := render.NewProgressBar(label)
	var stats []string
	sc := bufio.NewScanner(stdout)
	sc.Split(scanLinesCR)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case progressRE.MatchString(line):
			m := progressRE.FindStringSubmatch(line)
			pct, _ := strconv.Atoi(m[2])
			bar.Update(pct, m[3], m[4])
		case looksLikeStats(line):
			stats = append(stats, line)
		default:
			if f := fileLine(line); f != "" {
				bar.SetLabel(f)
			}
		}
	}
	bar.Finish()
	code := exitCode(cmd.Wait())
	if code == 0 {
		if parts := statsSummary(stats); len(parts) > 0 {
			render.Summary(label, parts)
		}
	}
	return code
}

// looksLikeStats matches the lines of rsync's --stats block (and the final
// sent/received/speedup lines), so they're collected rather than shown as files.
func looksLikeStats(line string) bool {
	for _, p := range []string{"Number of", "Total ", "Literal data", "Matched data", "File list", "sent ", "total size is"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// fileLine returns the transferred path from a -v filename line, or "" for
// blanks, directory entries (trailing /), and rsync's list-header noise.
func fileLine(line string) string {
	s := strings.TrimSpace(line)
	if s == "" || s == "./" || strings.HasSuffix(s, "/") {
		return ""
	}
	if strings.HasSuffix(s, "incremental file list") {
		return ""
	}
	return s
}

var (
	reStatFiles   = regexp.MustCompile(`Number of files:\s+[\d,]+\s+\(reg:\s+([\d,]+)`)
	reStatXfer    = regexp.MustCompile(`Number of (?:regular )?files transferred:\s+([\d,]+)`)
	reStatSize    = regexp.MustCompile(`Total file size:\s+([\d,]+)`)
	reStatRate    = regexp.MustCompile(`([\d,.]+)\s+bytes/sec`)
	reStatSpeedup = regexp.MustCompile(`speedup is\s+([\d.]+)`)
)

// statsSummary turns rsync's --stats block into house summary fields:
// transferred/total files (+ unchanged), total size, rate, speedup. Empty when
// nothing parses (e.g. no --stats output).
func statsSummary(stats []string) []string {
	j := strings.Join(stats, "\n")
	find := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(j); m != nil {
			return m[1]
		}
		return ""
	}
	var parts []string
	if total := commaInt(find(reStatFiles)); total > 0 {
		xfer := commaInt(find(reStatXfer))
		parts = append(parts, fmt.Sprintf("%d/%d files (%d unchanged)", xfer, total, total-xfer))
	}
	if size := commaInt(find(reStatSize)); size > 0 {
		parts = append(parts, render.HumanBytes(size))
	}
	if rate := commaFloat(find(reStatRate)); rate > 0 {
		parts = append(parts, render.HumanRate(rate))
	}
	if s := find(reStatSpeedup); s != "" && s != "0.00" {
		parts = append(parts, s+"× speedup")
	}
	return parts
}

func commaInt(s string) int64 {
	n, _ := strconv.ParseInt(strings.ReplaceAll(s, ",", ""), 10, 64)
	return n
}

func commaFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	return f
}

// scanLinesCR is a bufio.SplitFunc that tokenizes on either \r or \n; rsync
// overwrites the progress line with \r rather than \n.
func scanLinesCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// crackProgress splits tool-owned progress/verbose flags out of a token list.
// Long flags (--verbose/--progress/--info*) drop whole; short clusters keep their
// non-progress letters (-avuP → -au, stripping v and P).
func crackProgress(tokens []string) (kept, stripped []string) {
	for _, tok := range tokens {
		switch {
		case progressLong[tok] || strings.HasPrefix(tok, "--info"):
			stripped = append(stripped, tok)
		case len(tok) > 1 && tok[0] == '-' && tok[1] != '-':
			var bad, clean []rune
			for _, c := range tok[1:] {
				if c == 'v' || c == 'P' {
					bad = append(bad, c)
				} else {
					clean = append(clean, c)
				}
			}
			if len(bad) > 0 {
				for _, c := range bad {
					stripped = append(stripped, "-"+string(c))
				}
				if len(clean) > 0 {
					kept = append(kept, "-"+string(clean))
				}
			} else {
				kept = append(kept, tok)
			}
		default:
			kept = append(kept, tok)
		}
	}
	return kept, stripped
}

// sanitize drops tool-owned progress flags from a user layer, warning once.
func sanitize(tokens []string, source string) []string {
	kept, stripped := crackProgress(tokens)
	if len(stripped) > 0 {
		render.Warn(fmt.Sprintf("ignoring progress flags in %s (%s); the tool owns progress "+
			"(default aggregate bar; -v for per-file)", source, strings.Join(uniq(stripped), " ")))
	}
	return kept
}

// canonKeys returns the canonical option keys present in a raw token list.
func canonKeys(tokens []string) map[string]bool {
	keys := map[string]bool{}
	for _, tok := range tokens {
		head := strings.SplitN(tok, "=", 2)[0]
		if k, ok := longKey[head]; ok {
			keys[k] = true
		} else if len(tok) > 1 && tok[0] == '-' && tok[1] != '-' {
			for _, c := range tok[1:] {
				if k, ok := shortKey[c]; ok {
					keys[k] = true
				}
			}
		}
	}
	return keys
}

// shellSplit is a minimal quote-aware tokenizer for MU_HPC_RSYNC_OPTS (mirrors
// Python's shlex.split for the simple flag strings this env holds).
func shellSplit(s string) []string {
	var out []string
	var cur strings.Builder
	inWord := false
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			inWord = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}
