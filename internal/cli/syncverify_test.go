package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSha256Line(t *testing.T) {
	const h = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	// Text-mode ("  ") and binary-mode (" *") separators both parse; the path keeps spaces.
	if d, p, ok := parseSha256Line(h + "  field-a.nc"); !ok || d != h || p != "field-a.nc" {
		t.Errorf("text: d=%q p=%q ok=%v", d, p, ok)
	}
	if d, p, ok := parseSha256Line(h + " *nested/deep/b.nc"); !ok || d != h || p != "nested/deep/b.nc" {
		t.Errorf("binary: d=%q p=%q ok=%v", d, p, ok)
	}
	if _, p, ok := parseSha256Line(h + "  a b.nc"); !ok || p != "a b.nc" {
		t.Errorf("spaced path: p=%q ok=%v", p, ok)
	}
	// Rejects: short line, non-hex digest, a stray warning line.
	for _, bad := range []string{"", "too short", "zzzz" + h[4:] + "  x.nc", "sha256sum: x.nc: No such file or directory"} {
		if _, _, ok := parseSha256Line(bad); ok {
			t.Errorf("want reject: %q", bad)
		}
	}
}

func TestSha256File(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.bin")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sha256("hello")
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	h, n, err := sha256File(p)
	if err != nil || h != want || n != 5 {
		t.Errorf("sha256File = (%q, %d, %v), want (%q, 5, nil)", h, n, err, want)
	}
	if _, _, err := sha256File(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("missing file: want error")
	}
}
