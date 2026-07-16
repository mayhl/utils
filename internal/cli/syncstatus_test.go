package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestClassifyStatus covers the four drift buckets: in-sync (size + mtime match within
// the window), changed (size or mtime differs), missing locally, and unpushed.
func TestClassifyStatus(t *testing.T) {
	base := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	m := syncManifest{File: []manifestFile{
		{Path: "a.nc", Size: 10, Mtime: base.Format(time.RFC3339)},
		{Path: "b.nc", Size: 10, Mtime: base.Format(time.RFC3339)},
		{Path: "c.nc", Size: 10, Mtime: base.Format(time.RFC3339)},
		{Path: "d.nc", Size: 10, Mtime: base.Format(time.RFC3339)},
	}}
	local := map[string]localFile{
		"a.nc": {size: 10, mtime: base.Add(1 * time.Second)}, // inside the window — in sync
		"b.nc": {size: 99, mtime: base},                      // size differs — changed
		"c.nc": {size: 10, mtime: base.Add(time.Hour)},       // mtime outside the window — changed
		// d.nc absent — missing locally
		"e.nc": {size: 5, mtime: base}, // no manifest entry — unpushed
	}

	inSync, changed, missing, unpushed := classifyStatus(m, local)
	if len(inSync) != 1 || inSync[0] != "a.nc" {
		t.Errorf("inSync = %v", inSync)
	}
	if len(changed) != 2 || changed[0] != "b.nc" || changed[1] != "c.nc" {
		t.Errorf("changed = %v", changed)
	}
	if len(missing) != 1 || missing[0] != "d.nc" {
		t.Errorf("missing = %v", missing)
	}
	if len(unpushed) != 1 || unpushed[0] != "e.nc" {
		t.Errorf("unpushed = %v", unpushed)
	}
}

// TestClassifyStatusBadMtime: an unparseable recorded mtime falls back to size-only —
// matching size is in sync, not spuriously changed.
func TestClassifyStatusBadMtime(t *testing.T) {
	m := syncManifest{File: []manifestFile{{Path: "a.nc", Size: 10, Mtime: "garbled"}}}
	local := map[string]localFile{"a.nc": {size: 10, mtime: time.Now()}}
	inSync, changed, _, _ := classifyStatus(m, local)
	if len(inSync) != 1 || len(changed) != 0 {
		t.Errorf("size-only fallback: inSync=%v changed=%v", inSync, changed)
	}
}

// TestListTierFiles: the walk excludes the built-in junk set and the manifest's own
// basename, recurses, and returns slash rels; an absent dir is empty, not an error.
func TestListTierFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.nc", "aa")
	write("nested/b.nc", "bb")
	write(".DS_Store", "junk")
	write("x.tmp", "junk")
	write(syncManifestName, "copied-down manifest")

	got, err := listTierFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 files, got %v", got)
	}
	if f, ok := got["nested/b.nc"]; !ok || f.size != 2 {
		t.Errorf("nested/b.nc = %+v (ok=%v)", f, ok)
	}

	absent, err := listTierFiles(filepath.Join(dir, "nope"))
	if err != nil || len(absent) != 0 {
		t.Errorf("absent dir: %v, %v", absent, err)
	}
}
