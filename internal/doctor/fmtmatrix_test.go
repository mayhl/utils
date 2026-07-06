package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBareToolName(t *testing.T) {
	cases := map[string]string{
		"ruff":           "ruff",
		"pipx:fprettify": "fprettify",
		"pipx:cmakelang": "cmakelang",
		"ruff@0.15":      "ruff",
		"npm:prettier@3": "prettier",
		"golangci-lint":  "golangci-lint", // hyphen kept, no prefix
	}
	for in, want := range cases {
		if got := bareToolName(in); got != want {
			t.Errorf("bareToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolVersion(t *testing.T) {
	if got := toolVersion("0.15.20"); got != "0.15.20" {
		t.Errorf("string value = %q, want 0.15.20", got)
	}
	if got := toolVersion(map[string]any{"version": "1.2.3"}); got != "1.2.3" {
		t.Errorf("table value = %q, want 1.2.3", got)
	}
	if got := toolVersion(map[string]any{"other": "x"}); got != "" {
		t.Errorf("table without version = %q, want empty", got)
	}
}

func TestMasonReceiptVersion(t *testing.T) {
	dir := t.TempDir()
	write := func(id string) string {
		p := filepath.Join(dir, "r.json")
		if err := os.WriteFile(p, []byte(`{"source":{"id":"`+id+`"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		"pkg:pypi/ruff@0.15.12":                "0.15.12",
		"pkg:pypi/cmakelang@0.6.13?extra=YAML": "0.6.13",  // purl qualifier stripped
		"pkg:github/x/y@v3.13.1":               "v3.13.1", // v kept raw (normalized only at compare)
		"pkg:pypi/nover":                       "",        // no @version
	}
	for id, want := range cases {
		if got := masonReceiptVersion(write(id)); got != want {
			t.Errorf("masonReceiptVersion(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"3.13.1":       "3.13.1",
		"v3.13.1":      "3.13.1",
		"V2.5.2":       "2.5.2",
		" 0.11.0 ":     "0.11.0",
		"1.2.3+build7": "1.2.3",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCellStatus exercises the tier-aware verdict table: mise is opt-in, so a
// dormant mise is never itself an error and Mason is the sanctioned backup.
func TestCellStatus(t *testing.T) {
	cases := []struct {
		name   string
		cell   FmtCell
		tierOn bool
		want   Status
	}{
		{"both match", FmtCell{MiseCapable: true, MasonExpects: true, Mise: true, Mason: true}, false, OK},
		{"both drift", FmtCell{MiseCapable: true, MasonExpects: true, Mise: true, Mason: true, Drift: true}, false, Warn},
		{"mise only, mason expected, tier off", FmtCell{MiseCapable: true, MasonExpects: true, Mise: true}, false, Warn},
		{"mise only, mason expected, tier on", FmtCell{MiseCapable: true, MasonExpects: true, Mise: true}, true, Warn},
		{"mise only, no mason equiv, tier on (Go)", FmtCell{MiseCapable: true, MasonExpects: false, Mise: true}, true, OK},
		{"mise only, no mason equiv, tier off (Go)", FmtCell{MiseCapable: true, MasonExpects: false, Mise: true}, false, Warn},
		{"mason only, tier on → enforcement gap", FmtCell{MiseCapable: true, MasonExpects: true, Mason: true}, true, Warn},
		{"mason only, tier off → backup ok", FmtCell{MiseCapable: true, MasonExpects: true, Mason: true}, false, OK},
		{"neither", FmtCell{MiseCapable: true, MasonExpects: true}, false, Fail},
		{"mason-only cell present (LSP)", FmtCell{MiseCapable: false, MasonExpects: true, Mason: true}, false, OK},
		{"mason-only cell missing (LSP)", FmtCell{MiseCapable: false, MasonExpects: true}, false, Warn},
	}
	for _, c := range cases {
		if got := cellStatus(&c.cell, c.tierOn); got != c.want {
			t.Errorf("%s: cellStatus = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestFmtMatrix drives the whole build end-to-end against a temp config + fake
// Mason tree, checking derivation, the Mason probe, drift, and unknown-tool surfacing.
func TestFmtMatrix(t *testing.T) {
	cfgDir := t.TempDir()
	cfg := filepath.Join(cfgDir, "config.fmt.toml")
	body := `[tools]
ruff = "0.15.20"
gofumpt = "0.10.0"
"pipx:fprettify" = "0.3.7"
sometool = "1.0.0"
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mason := t.TempDir()
	writePkg := func(name, id string) {
		d := filepath.Join(mason, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "mason-receipt.json"),
			[]byte(`{"source":{"id":"`+id+`"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writePkg("ruff", "pkg:pypi/ruff@0.15.12")         // drift vs mise 0.15.20
	writePkg("fprettify", "pkg:pypi/fprettify@0.3.7") // matches mise
	writePkg("pyright", "pkg:npm/pyright@1.1.0")      // supplement cell present

	t.Setenv("MU_MISE_FMT_CONFIG", cfg)
	t.Setenv("MU_MASON_DIR", mason)
	t.Setenv("MU_MISE_FMT", "") // tier off
	t.Setenv("MISE_ENV", "")

	rep := FmtMatrix()
	if !rep.ConfigOK {
		t.Fatal("ConfigOK = false, want true")
	}
	if rep.TierOn {
		t.Error("TierOn = true, want false")
	}

	cell := func(lang string, role Role) FmtCell {
		for _, r := range rep.Rows {
			if r.Lang == lang {
				return r.Cells[int(role)]
			}
		}
		t.Fatalf("no row for %s", lang)
		return FmtCell{}
	}

	// Python Format: ruff in both, versions differ → drift → Warn.
	py := cell("Python", Format)
	if !py.Mise || !py.Mason || !py.Drift || py.Status != Warn {
		t.Errorf("Python/Format = %+v, want mise+mason+drift+warn", py)
	}
	// Fortran Format: fprettify both, versions match → OK, no drift.
	fo := cell("Fortran", Format)
	if !fo.Mise || !fo.Mason || fo.Drift || fo.Status != OK {
		t.Errorf("Fortran/Format = %+v, want mise+mason, no drift, ok", fo)
	}
	// Go Format: gofumpt mise-only, no Mason equiv, tier off → Warn.
	gc := cell("Go", Format)
	if !gc.Mise || gc.Mason || gc.Status != Warn {
		t.Errorf("Go/Format = %+v, want mise-only, warn (tier off)", gc)
	}
	// Python LSP: pyright supplement, present in Mason → OK.
	lsp := cell("Python", LSP)
	if lsp.Mise || !lsp.Mason || lsp.Status != OK {
		t.Errorf("Python/LSP = %+v, want mason-only, ok", lsp)
	}
	// Unknown tool surfaced.
	found := false
	for _, u := range rep.Unknown {
		if u == "sometool" {
			found = true
		}
	}
	if !found {
		t.Errorf("Unknown = %v, want it to contain sometool", rep.Unknown)
	}
}
