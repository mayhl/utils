package queue

import "strings"

// ClassifyQueue infers a queue's class from its name: GPU, VIS (visualization), BigMem
// (large-memory CPU), Xfer (data-movement queues — transfer + archive/HPSS, not compute),
// or CPU (the default). It's a first pass that matches only GENERIC tokens (no site-specific
// queue names — those stay in the untracked config override), so it's safe to ship in a
// public repo. A config queue→class override (applied by the caller) corrects specialty
// queues the tokens misread. BigMem is a CPU subtype; Xfer folds transfer and archive into
// one data-movement class (they're the same idea and rarely coexist).
func ClassifyQueue(name string) string {
	n := strings.ToLower(name)
	switch {
	case containsAny(n, "gpu"):
		return "GPU"
	case containsAny(n, "vis", "viz"):
		return "VIS"
	case containsAny(n, "bigmem", "bmem", "largemem", "lgmem", "himem"),
		hasSuffixToken(n, "bm"), hasSuffixToken(n, "hm"):
		return "BigMem"
	case containsAny(n, "transfer", "xfer", "archive", "hpss"):
		return "Xfer"
	default:
		return "CPU"
	}
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// hasSuffixToken reports whether s is exactly tok or ends with tok on a "_"/"-" separator
// boundary (e.g. "standard_bm", "cpu-hm"). Used for short big-memory suffixes like bm/hm
// that would false-positive as bare substrings ("submit" contains "bm").
func hasSuffixToken(s, tok string) bool {
	return s == tok || strings.HasSuffix(s, "_"+tok) || strings.HasSuffix(s, "-"+tok)
}
