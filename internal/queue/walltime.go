package queue

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Walltime, in the one place that knows how to read it.
//
// The schedulers take H+:MM:SS and nothing else, but nobody thinks in it — an interactive
// session is "an hour", a test is "ten minutes". So mu accepts a duration shorthand and
// normalizes on the way out: what reaches qsub/sbatch is always canonical.

var (
	wallCanon = regexp.MustCompile(`^\d+:[0-5]\d:[0-5]\d$`)
	wallPart  = regexp.MustCompile(`(\d+(?:\.\d+)?)([dhms])`)
)

// ParseWalltime reads H+:MM:SS or a shorthand ("10m", "1.5h", "1h30m", "2d") into seconds.
//
// A BARE NUMBER is refused on purpose: PBS reads `walltime=10` as ten SECONDS, which no one
// has ever meant. Requiring the unit costs one keystroke and removes the whole class of
// mistake — a job that dies instantly, having queued for an hour.
func ParseWalltime(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	if wallCanon.MatchString(s) {
		p := strings.Split(s, ":")
		h, _ := strconv.Atoi(p[0])
		m, _ := strconv.Atoi(p[1])
		sec, _ := strconv.Atoi(p[2])
		return h*3600 + m*60 + sec, true
	}
	// The shorthand must be ENTIRELY units — matching the parts is not enough, or "1h junk"
	// and "90" would both quietly parse as something.
	spans := wallPart.FindAllStringSubmatchIndex(s, -1)
	if len(spans) == 0 {
		return 0, false
	}
	total, at := 0.0, 0
	for _, sp := range spans {
		if sp[0] != at {
			return 0, false // unconsumed text between the parts
		}
		at = sp[1]
		n, err := strconv.ParseFloat(s[sp[2]:sp[3]], 64)
		if err != nil {
			return 0, false
		}
		switch s[sp[4]:sp[5]] {
		case "d":
			total += n * 86400
		case "h":
			total += n * 3600
		case "m":
			total += n * 60
		case "s":
			total += n
		}
	}
	if at != len(s) || total < 0 {
		return 0, false
	}
	return int(math.Round(total)), true
}

// FormatWalltime renders seconds as the canonical H+:MM:SS. Hours are NOT rolled into days:
// a 168-hour limit prints as 168:00:00, which is how the centers themselves write it.
func FormatWalltime(sec int) string {
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", sec/3600, (sec%3600)/60, sec%60)
}

// NormalizeWalltime is Parse then Format: what the user typed, in the form the scheduler
// takes. Empty in, empty out and ok — a blank walltime is a legitimate "don't send one".
func NormalizeWalltime(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return "", true
	}
	sec, ok := ParseWalltime(s)
	if !ok {
		return "", false
	}
	return FormatWalltime(sec), true
}

// parsePBSDuration reads a PBS qstat time cell into seconds. The wide format prints Req'd
// Time as HH:MM and Elapsed as HH:MM:SS, so a two-field cell is HOURS:MINUTES — the opposite
// of SLURM, where two fields are minutes:seconds. This is why the reader must know the
// dialect: "24:00" is 24 hours here and 24 minutes there.
func parsePBSDuration(s string) (int, bool) {
	f, ok := clockFields(s)
	if !ok {
		return 0, false
	}
	switch len(f) {
	case 2: // HH:MM
		return f[0]*3600 + f[1]*60, true
	case 3: // HH:MM:SS
		return f[0]*3600 + f[1]*60 + f[2], true
	}
	return 0, false
}

// parseSLURMDuration reads a squeue time cell into seconds. squeue picks the SHORTEST form
// that fits the magnitude: MM:SS under an hour, HH:MM:SS under a day, D-HH:MM:SS beyond — so
// a two-field cell is MINUTES:SECONDS, and a leading "D-" adds whole days. A non-clock value
// (UNLIMITED, N/A, NOT_SET, a blank) is not a duration and returns not-ok, so the column
// degrades rather than inventing a number.
func parseSLURMDuration(s string) (int, bool) {
	s = strings.TrimSpace(s)
	days := 0
	if i := strings.IndexByte(s, '-'); i > 0 { // D-HH:MM:SS
		d, err := strconv.Atoi(s[:i])
		if err != nil || d < 0 {
			return 0, false
		}
		days, s = d, s[i+1:]
	}
	f, ok := clockFields(s)
	if !ok {
		return 0, false
	}
	switch {
	case days > 0 && len(f) == 3: // D-HH:MM:SS — the day form is always full
		return days*86400 + f[0]*3600 + f[1]*60 + f[2], true
	case days > 0:
		return 0, false // a day prefix without a full HH:MM:SS is malformed
	case len(f) == 2: // MM:SS
		return f[0]*60 + f[1], true
	case len(f) == 3: // HH:MM:SS
		return f[0]*3600 + f[1]*60 + f[2], true
	}
	return 0, false
}

// clockFields splits a colon clock into its integer fields (2 or 3), rejecting anything
// non-numeric — so UNLIMITED, N/A and a blank cell fall through instead of parsing as zero.
func clockFields(s string) ([]int, bool) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return nil, false
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}
