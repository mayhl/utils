package queue

import "testing"

func TestClassifyQueue(t *testing.T) {
	cases := map[string]string{
		"standard":     "CPU",
		"debug":        "CPU",
		"gpu":          "GPU",
		"gpu_standard": "GPU",
		"v100":         "CPU", // hardware names aren't matched — config override handles those
		"vis":          "VIS",
		"viz_long":     "VIS",
		"bigmem":       "BigMem",
		"bmem":         "BigMem",
		"standard_bm":  "BigMem", // short suffix on a separator boundary
		"cpu-hm":       "BigMem",
		"bm":           "BigMem",
		"submit":       "CPU", // must NOT match the "bm" substring
		"cpu_bmark":    "CPU", // "bm" mid-token, not a suffix → CPU
		"transfer":     "Xfer",
		"xfer":         "Xfer",
		"archive":      "Xfer", // transfer + archive merged into one data-movement class
		"hpss":         "Xfer",
	}
	for name, want := range cases {
		if got := ClassifyQueue(name); got != want {
			t.Errorf("ClassifyQueue(%q) = %q, want %q", name, got, want)
		}
	}
}
