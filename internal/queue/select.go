package queue

import (
	"regexp"
	"strconv"
	"strings"
)

// Selector grammar for picking jobs to cancel, mirroring the process selector
// (proc.Classify): a token is an id/range/list when it's all-numeric with the right
// shape, else a name mask. This is where REVERSE short-id resolution lives — the
// short id `1284570` you see in `mstat` resolves back to the full `1284570.hpc1`
// that `qdel` needs. A leading `~` forces a token to be a name mask.
var (
	reSingle = regexp.MustCompile(`^[0-9]+$`)
	reRange  = regexp.MustCompile(`^([0-9]+)-([0-9]+)$`)
	reList   = regexp.MustCompile(`^[0-9]+(,[0-9]+)+$`)
	reLead   = regexp.MustCompile(`^[0-9]+`)
)

// Match returns the jobs a single selector token picks. forcePattern (from -p) makes
// it a name mask regardless of shape.
func Match(jobs []Job, token string, forcePattern bool) []Job {
	if forcePattern {
		return matchMask(jobs, token)
	}
	if strings.HasPrefix(token, "~") {
		return matchMask(jobs, token[1:])
	}
	switch {
	case reSingle.MatchString(token):
		return matchID(jobs, func(j Job) bool { return idMatches(j, token) })
	case reRange.MatchString(token):
		m := reRange.FindStringSubmatch(token)
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		if lo > hi {
			lo, hi = hi, lo
		}
		return matchID(jobs, func(j Job) bool {
			n, ok := leadNum(j.ShortID)
			return ok && n >= lo && n <= hi
		})
	case reList.MatchString(token):
		want := make(map[string]bool)
		for _, s := range strings.Split(token, ",") {
			want[s] = true
		}
		return matchID(jobs, func(j Job) bool {
			for id := range want {
				if idMatches(j, id) {
					return true
				}
			}
			return false
		})
	default:
		// A full native id (e.g. "1284570.hpc1") isn't numeric-shaped but must still
		// match by id, not name.
		if byID := matchID(jobs, func(j Job) bool { return j.ID == token }); len(byID) > 0 {
			return byID
		}
		return matchMask(jobs, token)
	}
}

// MatchAll resolves several selector tokens against jobs and returns their deduped
// union (a job matched by two tokens is cancelled once), preserving job order.
func MatchAll(jobs []Job, tokens []string, forcePattern bool) []Job {
	seen := make(map[string]bool)
	var out []Job
	for _, tok := range tokens {
		for _, j := range Match(jobs, tok, forcePattern) {
			if !seen[j.ID] {
				seen[j.ID] = true
				out = append(out, j)
			}
		}
	}
	return out
}

// idMatches reports whether a numeric token names this job — by short id, by the
// short id's leading number (array ids like "1284[7]"), or by the full native id.
func idMatches(j Job, token string) bool {
	if j.ShortID == token || j.ID == token {
		return true
	}
	if n, ok := leadNum(j.ShortID); ok {
		return strconv.Itoa(n) == token
	}
	return false
}

func matchID(jobs []Job, pred func(Job) bool) []Job {
	var out []Job
	for _, j := range jobs {
		if pred(j) {
			out = append(out, j)
		}
	}
	return out
}

// matchMask matches token against the job Name as a regexp, falling back to substring
// if the pattern isn't a valid regexp.
func matchMask(jobs []Job, pat string) []Job {
	re, err := regexp.Compile(pat)
	var out []Job
	for _, j := range jobs {
		hit := strings.Contains(j.Name, pat)
		if err == nil {
			hit = re.MatchString(j.Name)
		}
		if hit {
			out = append(out, j)
		}
	}
	return out
}

// leadNum is the leading integer of a (short) id: "1284570" → 1284570,
// "1284[7]" → 1284. Reports false when there's no leading digit run.
func leadNum(s string) (int, bool) {
	m := reLead.FindString(s)
	if m == "" {
		return 0, false
	}
	n, err := strconv.Atoi(m)
	return n, err == nil
}
