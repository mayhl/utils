package cli

import (
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// TestMergeManifestAppendAndReplace covers the two folds: a new file appends, a re-pushed
// path (the --force overwrite case) replaces its entry in place, and the result stays
// sorted by path.
func TestMergeManifestAppendAndReplace(t *testing.T) {
	existing := syncManifest{File: []manifestFile{
		{Path: "b.nc", Size: 10, Commit: "old"},
		{Path: "a.nc", Size: 20, Commit: "old"},
	}}
	merged := mergeManifest(existing, []manifestFile{
		{Path: "a.nc", Size: 99, Commit: "new"}, // replace
		{Path: "c.nc", Size: 30, Commit: "new"}, // append
	})

	if len(merged.File) != 3 {
		t.Fatalf("want 3 files, got %d", len(merged.File))
	}
	want := []string{"a.nc", "b.nc", "c.nc"} // sorted
	for i, w := range want {
		if merged.File[i].Path != w {
			t.Errorf("file[%d] = %q, want %q", i, merged.File[i].Path, w)
		}
	}
	if merged.File[0].Size != 99 || merged.File[0].Commit != "new" {
		t.Errorf("a.nc not replaced: %+v", merged.File[0])
	}
	if merged.File[1].Commit != "old" {
		t.Errorf("b.nc mutated: %+v", merged.File[1])
	}
}

// TestManifestRoundTrip confirms the manifest marshals to [[file]] tables and parses back
// unchanged — read-modify-write depends on the fetched TOML round-tripping cleanly.
func TestManifestRoundTrip(t *testing.T) {
	in := syncManifest{File: []manifestFile{
		{Path: "bathy.nc", Size: 4096, Mtime: "2026-07-16T10:00:00Z", SHA256: "abc", Commit: "1af6972", Dirty: false, Synced: "2026-07-16T10:01:00Z", Host: "box"},
	}}
	body, err := toml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "[[file]]") {
		t.Errorf("no [[file]] table in:\n%s", body)
	}
	var out syncManifest
	if err := toml.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.File) != 1 || out.File[0] != in.File[0] {
		t.Errorf("round trip changed the entry: %+v", out.File)
	}
}

// TestMergeManifestEmptyBase covers the first-push path: no existing manifest, entries land
// as-is (sorted).
func TestMergeManifestEmptyBase(t *testing.T) {
	merged := mergeManifest(syncManifest{}, []manifestFile{
		{Path: "z.nc"}, {Path: "a.nc"},
	})
	if len(merged.File) != 2 || merged.File[0].Path != "a.nc" {
		t.Errorf("empty-base merge wrong: %+v", merged.File)
	}
}
