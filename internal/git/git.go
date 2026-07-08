// Package git backs the `mu git` module: read-only PREVIEWS of the .config
// signwip / pushsigned workflow — what WOULD sign or push — plus a doctor check.
// It never mutates history; the shell `gsw`/`gps` stay the source of truth and do
// the actual signing/pushing (bootstrap-safe: signing mu's own repo never depends
// on a working mu). All queries shell out to `git` in the current directory.
package git

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unreviewed = "[unreviewed] "

// out runs `git args...` in the CWD and returns trimmed stdout; non-zero exit → err.
func out(args ...string) (string, error) {
	b, err := exec.Command("git", args...).Output()
	return strings.TrimSpace(string(b)), err
}

// WipRow is one line of the signwip preview. The base (newest signed) row carries
// Act "base"; the WIP stacked on it carries "sign" (untagged) or "skip" ([unreviewed]).
type WipRow struct {
	Act     string // "base" | "sign" | "skip"
	Hash    string // short
	Subject string
}

// Signwip is the read-only signwip preview: the newest signed commit (base) and the
// unsigned WIP stacked on it, each marked sign (untagged) or skip ([unreviewed]).
type Signwip struct {
	HasBase bool
	Rows    []WipRow
	ToSign  int
	Tagged  int
	Total   int
}

// signedBase returns the newest Good-signed commit hash. It STREAMS git log and stops
// at the first match, closing the pipe so git (and its per-commit gpg verify) halts at
// the base instead of walking all of history — mirroring the shell's `... | awk '…exit'`.
// Reading the whole output instead would gpg-verify every commit (seconds on real repos).
func signedBase() (string, error) {
	cmd := exec.Command("git", "log", "--format=%H %G?")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	base := ""
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) >= 2 && f[1] == "G" {
			base = f[0]
			break
		}
	}
	_ = stdout.Close() // stop git early (SIGPIPE); ignore the resulting wait error
	_ = cmd.Wait()
	return base, nil
}

// SignwipPreview mirrors git-signwip.sh's preview without signing anything.
func SignwipPreview() (Signwip, error) {
	var s Signwip
	base, err := signedBase()
	if err != nil {
		return s, err
	}
	if base == "" {
		return s, nil // no signed base — HasBase stays false
	}
	s.HasBase = true
	if bh, err := out("log", "-1", "--format=%h%x09%s", base); err == nil {
		if h, sub, ok := strings.Cut(bh, "\t"); ok {
			s.Rows = append(s.Rows, WipRow{Act: "base", Hash: h, Subject: sub})
		}
	}
	wip, err := out("log", "--reverse", "--format=%h%x09%s", base+"..HEAD")
	if err != nil {
		return s, err
	}
	for _, line := range splitNonEmpty(wip) {
		h, sub, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		s.Total++
		act := "sign"
		if strings.HasPrefix(sub, unreviewed) {
			act = "skip"
			s.Tagged++
		}
		s.Rows = append(s.Rows, WipRow{Act: act, Hash: h, Subject: sub})
	}
	s.ToSign = s.Total - s.Tagged
	return s, nil
}

// ReviewRow is one line of the reviewed preview: a commit in the unsigned WIP stack
// above the signed base. Act is "untag" (an [unreviewed] commit among the oldest-N that
// `git reviewed` would clear), "keep" (an [unreviewed] commit beyond N, left tagged),
// "clean" (already untagged), or "base" (the newest signed commit).
type ReviewRow struct {
	Act     string
	Hash    string
	Subject string
}

// Reviewed is the read-only `git reviewed [N]` preview: the WIP above the signed base,
// with the oldest-N [unreviewed] commits marked untag.
type Reviewed struct {
	HasBase bool
	Rows    []ReviewRow
	Tagged  int // total [unreviewed] above base
	Untag   int // how many the oldest-N selection clears
}

// ReviewedPreview mirrors git-reviewed.sh's preview without un-tagging anything: it marks
// the oldest n [unreviewed] commits above the signed base as untag (n<=0 → all of them),
// the rest keep, oldest-first.
func ReviewedPreview(n int) (Reviewed, error) {
	var r Reviewed
	base, err := signedBase()
	if err != nil {
		return r, err
	}
	if base == "" {
		return r, nil // no signed base — HasBase stays false
	}
	r.HasBase = true
	if bh, err := out("log", "-1", "--format=%h%x09%s", base); err == nil {
		if h, sub, ok := strings.Cut(bh, "\t"); ok {
			r.Rows = append(r.Rows, ReviewRow{Act: "base", Hash: h, Subject: sub})
		}
	}
	wip, err := out("log", "--reverse", "--format=%h%x09%s", base+"..HEAD")
	if err != nil {
		return r, err
	}
	rows, tagged, untag := classifyWip(splitNonEmpty(wip), n)
	r.Rows = append(r.Rows, rows...)
	r.Tagged, r.Untag = tagged, untag
	return r, nil
}

// classifyWip marks the WIP log lines ("hash\tsubject", oldest→newest): the oldest n
// [unreviewed] commits as untag (n<=0 or n>tagged → all of them), any later [unreviewed]
// as keep, and already-untagged commits as clean. Returns the rows plus the total tagged
// count and how many were selected to untag. Pure (no git) so the selection is unit-tested.
func classifyWip(lines []string, n int) (rows []ReviewRow, tagged, untag int) {
	for _, line := range lines {
		if _, sub, ok := strings.Cut(line, "\t"); ok && strings.HasPrefix(sub, unreviewed) {
			tagged++
		}
	}
	lim := n
	if lim <= 0 || lim > tagged {
		lim = tagged
	}
	for _, line := range lines {
		h, sub, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		act := "clean"
		if strings.HasPrefix(sub, unreviewed) {
			if untag < lim {
				act, untag = "untag", untag+1
			} else {
				act = "keep"
			}
		}
		rows = append(rows, ReviewRow{Act: act, Hash: h, Subject: sub})
	}
	return rows, tagged, untag
}

// PushRow is one line of the pushsigned preview.
type PushRow struct {
	Push    bool // in the contiguous signed prefix → would push
	Signed  bool // %G? == G
	Hash    string
	Subject string
}

// Pushsigned is the read-only pushsigned preview: the commits ahead of @{u}, with
// the contiguous signed prefix marked push and the rest held local.
type Pushsigned struct {
	HasUpstream bool
	Upstream    string
	Rows        []PushRow
	PushN       int
	Held        int
}

// PushsignedPreview mirrors git-pushsigned.sh's preview without pushing anything.
func PushsignedPreview() (Pushsigned, error) {
	var p Pushsigned
	up, err := out("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil || up == "" {
		return p, nil // no upstream — HasUpstream stays false
	}
	p.HasUpstream = true
	p.Upstream = up
	raw, err := out("log", "--reverse", "--format=%h%x09%G?%x09%s", "@{u}..HEAD")
	if err != nil {
		return p, err
	}
	inPrefix := true
	for _, line := range splitNonEmpty(raw) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		signed := parts[1] == "G"
		if inPrefix && signed {
			p.Rows = append(p.Rows, PushRow{Push: true, Signed: true, Hash: parts[0], Subject: parts[2]})
			p.PushN++
			continue
		}
		inPrefix = false
		p.Rows = append(p.Rows, PushRow{Push: false, Signed: signed, Hash: parts[0], Subject: parts[2]})
		p.Held++
	}
	return p, nil
}

// FileCheck is one existence check for the doctor report.
type FileCheck struct {
	Name   string
	Path   string
	Exists bool
}

// Doctor is the read-only `mu git doctor` report: git on PATH and the .config git
// workflow files present. Existence only — the files are not validated.
type Doctor struct {
	GitPath string // resolved `git` path, or "" if not found
	Files   []FileCheck
}

// DoctorReport checks git-on-PATH plus the ~/.config git config and workflow scripts.
func DoctorReport() Doctor {
	var d Doctor
	d.GitPath, _ = exec.LookPath("git")
	cfg := configHome()
	for _, c := range []struct{ name, path string }{
		{"git config", filepath.Join(cfg, "git", "config")},
		{"git-commitinc.sh", filepath.Join(cfg, "scripts", "git-commitinc.sh")},
		{"git-reviewed.sh", filepath.Join(cfg, "scripts", "git-reviewed.sh")},
		{"git-signwip.sh", filepath.Join(cfg, "scripts", "git-signwip.sh")},
		{"git-pushsigned.sh", filepath.Join(cfg, "scripts", "git-pushsigned.sh")},
	} {
		_, err := os.Stat(c.path)
		d.Files = append(d.Files, FileCheck{Name: c.name, Path: c.path, Exists: err == nil})
	}
	return d
}

func configHome() string {
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// splitNonEmpty splits on newlines, dropping the empty tail from an empty string.
func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
