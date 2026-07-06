package proc

import (
	"regexp"
	"strconv"
	"strings"
)

// Kind classifies a selector token by SHAPE (see Classify). The same grammar will
// serve the queue plane's `mdel`; short-id resolution (jobids) is added there, not
// needed here since a PID is already the whole id.
type Kind int

const (
	Mask     Kind = iota // grep-style name pattern (pgrep-faithful)
	IDSingle             // one PID
	IDRange              // inclusive PID range lo-hi
	IDList               // explicit PID list
)

// Selector is a parsed target spec: a mask pattern, or a set of PIDs.
type Selector struct {
	Kind Kind
	Pat  string // Mask
	Lo   int    // IDRange
	Hi   int    // IDRange
	IDs  []int  // IDSingle (one) / IDList
}

var (
	reSingle = regexp.MustCompile(`^[0-9]+$`)
	reRange  = regexp.MustCompile(`^([0-9]+)-([0-9]+)$`)
	reList   = regexp.MustCompile(`^[0-9]+(,[0-9]+)+$`)
)

// Classify maps a token to a Selector by shape: a leading `~` forces a mask (for a
// numeric that's really a name); otherwise all-numeric with range/list syntax is an
// id selector, and anything else is a grep mask.
func Classify(tok string) Selector {
	if strings.HasPrefix(tok, "~") {
		return Selector{Kind: Mask, Pat: tok[1:]}
	}
	switch {
	case reSingle.MatchString(tok):
		n, _ := strconv.Atoi(tok)
		return Selector{Kind: IDSingle, IDs: []int{n}}
	case reRange.MatchString(tok):
		m := reRange.FindStringSubmatch(tok)
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		if lo > hi {
			lo, hi = hi, lo
		}
		return Selector{Kind: IDRange, Lo: lo, Hi: hi}
	case reList.MatchString(tok):
		var ids []int
		for _, s := range strings.Split(tok, ",") {
			n, _ := strconv.Atoi(s)
			ids = append(ids, n)
		}
		return Selector{Kind: IDList, IDs: ids}
	default:
		return Selector{Kind: Mask, Pat: tok}
	}
}

// Match returns the processes selected by sel. A Mask is matched as a regexp
// against the process Name (pgrep-style), falling back to substring if the pattern
// isn't a valid regexp.
func (sel Selector) Match(ps []Process) []Process {
	var out []Process
	switch sel.Kind {
	case Mask:
		re, err := regexp.Compile(sel.Pat)
		for _, p := range ps {
			hit := strings.Contains(p.Name, sel.Pat)
			if err == nil {
				hit = re.MatchString(p.Name)
			}
			if hit {
				out = append(out, p)
			}
		}
	case IDRange:
		for _, p := range ps {
			if p.PID >= sel.Lo && p.PID <= sel.Hi {
				out = append(out, p)
			}
		}
	default: // IDSingle / IDList
		want := make(map[int]bool, len(sel.IDs))
		for _, id := range sel.IDs {
			want[id] = true
		}
		for _, p := range ps {
			if want[p.PID] {
				out = append(out, p)
			}
		}
	}
	return out
}
