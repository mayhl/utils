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
