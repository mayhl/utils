package cli

import (
	"strings"
	"testing"
)

// TestParseItemize covers the 11-char flag / path split: only real itemized change
// lines parse, and only known file-type codes are accepted.
func TestParseItemize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		line     string
		wantOK   bool
		wantItem string
		wantPath string
	}{
		{"new file", ">f+++++++++ data/bathy.nc", true, ">f+++++++++", "data/bathy.nc"},
		{"changed file", ">f.st...... data/forcing.nc", true, ">f.st......", "data/forcing.nc"},
		{"new dir", "cd+++++++++ data/sub/", true, "cd+++++++++", "data/sub/"},
		{"attr-only file", ".f...p..... data/keep.nc", true, ".f...p.....", "data/keep.nc"},
		{"path with spaces", ">f+++++++++ a b/c d.nc", true, ">f+++++++++", "a b/c d.nc"},
		{"blank", "", false, "", ""},
		{"stats line", "Number of files: 12", false, "", ""},
		{"short", ">f+++++++++", false, "", ""},
		{"bad filetype", ">z+++++++++ x", false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item, path, ok := parseItemize(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && (item != tc.wantItem || path != tc.wantPath) {
				t.Fatalf("got (%q, %q), want (%q, %q)", item, path, tc.wantItem, tc.wantPath)
			}
		})
	}
}

// TestClassifyItemize covers the new/update split on real push itemize (leading '<',
// as rsync emits when sending to a remote): all-'+' files are new, sent files with real
// change flags are updates, and dirs / attr-only lines are ignored.
func TestClassifyItemize(t *testing.T) {
	out := strings.Join([]string{
		"cd+++++++++ ./",       // dir create — ignored
		"<f+++++++++ a.nc",     // new (push)
		"<f+++++++++ sub/b.nc", // new
		"<f.s....... c.nc",     // exists, size differs — update
		"<f..t...... d.nc",     // exists, time differs — update
		".f...p..... e.nc",     // attr-only, not transferred — ignored
		"cd+++++++++ sub/",     // dir — ignored
		"Number of files: 5",   // stats noise — ignored
	}, "\n")

	newN, updates := classifyItemize([]byte(out))
	if newN != 2 {
		t.Errorf("newN = %d, want 2", newN)
	}
	wantUpd := []string{"c.nc", "d.nc"}
	if len(updates) != len(wantUpd) {
		t.Fatalf("updates = %v, want %v", updates, wantUpd)
	}
	for i, u := range wantUpd {
		if updates[i] != u {
			t.Errorf("updates[%d] = %q, want %q", i, updates[i], u)
		}
	}
}
