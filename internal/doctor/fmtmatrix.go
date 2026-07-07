package doctor

// mu doctor fmt — a language × role matrix of the formatter/linter/debug/LSP
// stack, each cell judged by which source provides the tool. Two stacks are in
// play: the mise `fmt` tier (an EMBEDDED default declared-tool set, overridable at
// ~/.config/mu/config.fmt.toml — the ENFORCEMENT copy behind the git hook and
// `mu fmt`) and Mason (nvim's EDITOR copy). The fmt tier is opt-in, so a dormant
// mise is never itself an error: verdicts are tier-aware and Mason is the
// sanctioned backup (see cellStatus).
//
// Format/Lint cells are DERIVED from the declared-tool config (a tool declared
// there lights its mise badge); the classifier only labels each known tool with its
// language, role, and Mason package. Debug/LSP (and a couple of linters) have no
// mise counterpart at all — mise manages no debugger/LSP — so they come from a small
// static supplement, Mason-only by nature.

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// defaultFmtConfig is the built-in declared-tool set, embedded so `mu doctor fmt`
// works with no external file. A user override (see fmtConfigPath) fully replaces it.
//
//go:embed default.fmt.toml
var defaultFmtConfig []byte

// Role is a tooling column in the fmt matrix.
type Role int

const (
	Format Role = iota
	Lint
	Debug
	LSP
)

// RoleOrder is the column order; RoleNames labels them.
var RoleOrder = []Role{Format, Lint, Debug, LSP}

func (r Role) String() string {
	switch r {
	case Format:
		return "Format"
	case Lint:
		return "Lint"
	case Debug:
		return "Debug"
	case LSP:
		return "LSP"
	}
	return "?"
}

// langOrder groups the scientific stack first, then the tooling languages.
var langOrder = []string{"Python", "Fortran", "CMake", "Lua", "Shell", "Go"}

// roleSpec places one role of a tool: its display name and the Mason package that
// mirrors it (empty when Mason has no equivalent, e.g. Go's gofumpt/golangci-lint).
type roleSpec struct {
	role     Role
	display  string
	masonPkg string
}

// toolSpec is a mise-managed tool's classification, keyed in `classifier` by its
// bare name (backend prefix and version stripped). A tool contributes its cells
// only when config.fmt.toml declares it — that's the "derive from config" gate.
type toolSpec struct {
	lang  string
	roles []roleSpec
}

var classifier = map[string]toolSpec{
	"gofumpt":       {"Go", []roleSpec{{Format, "gofumpt", ""}}},
	"golangci-lint": {"Go", []roleSpec{{Lint, "golangci-lint", ""}}},
	"shfmt":         {"Shell", []roleSpec{{Format, "shfmt", "shfmt"}}},
	"shellcheck":    {"Shell", []roleSpec{{Lint, "shellcheck", "shellcheck"}}},
	"stylua":        {"Lua", []roleSpec{{Format, "stylua", "stylua"}}},
	"ruff":          {"Python", []roleSpec{{Format, "ruff", "ruff"}, {Lint, "ruff", "ruff"}}},
	"fprettify":     {"Fortran", []roleSpec{{Format, "fprettify", "fprettify"}}},
	"cmakelang":     {"CMake", []roleSpec{{Format, "cmake-format", "cmakelang"}, {Lint, "cmake-lint", "cmakelang"}}},
}

// supplement are Mason-only cells: mise manages no tool for these (lang, role)
// slots, so Mason (or nothing) is the sole possible source.
var supplement = []struct {
	lang     string
	role     Role
	display  string
	masonPkg string
}{
	{"Python", Debug, "debugpy", "debugpy"},
	{"Python", LSP, "pyright", "pyright"},
	{"Fortran", Lint, "fortitude", "fortitude"},
	{"Fortran", LSP, "fortls", "fortls"},
	{"Lua", Lint, "luacheck", "luacheck"},
	{"Lua", LSP, "lua-language-server", "lua-language-server"},
	{"Shell", LSP, "bash-language-server", "bash-language-server"},
}

// FmtCell is one (language, role) slot. Undefined cells (nothing tracked for the
// slot) render as a dash and don't count toward the row/overall verdict.
type FmtCell struct {
	Defined      bool
	MiseCapable  bool // mise could own this slot (Format/Lint); false for Debug/LSP-style cells
	MasonExpects bool // a Mason equivalent exists in principle (masonPkg set)
	Tool         string
	Mise         bool // declared in config.fmt.toml
	Mason        bool // installed under mason/packages
	MiseVer      string
	MasonVer     string
	Drift        bool // present in both, versions disagree
	Status       Status
}

// FmtRow is one language's cells across RoleOrder.
type FmtRow struct {
	Lang   string
	Cells  []FmtCell
	Status Status
}

// FmtReport is the whole matrix plus its context.
type FmtReport struct {
	TierOn     bool
	Rows       []FmtRow
	Status     Status
	ConfigPath string
	ConfigOK   bool
	Unknown    []string // tools declared in config that the classifier doesn't know
}

// FmtMatrix builds the fmt tooling matrix: parse config.fmt.toml for the mise
// (enforced) side, probe Mason for the editor side, and judge each cell tier-aware.
func FmtMatrix() FmtReport {
	tierOn := tierActive()
	data, path := effectiveFmtConfig()
	declared, ok := parseFmtConfigBytes(data)

	// Track which classifier tools the config lit up, to flag unknown extras.
	known := map[string]bool{}

	// index[lang][role] → *FmtCell, over the fixed grid.
	rows := make([]FmtRow, len(langOrder))
	index := map[string]*FmtRow{}
	for i, lang := range langOrder {
		rows[i] = FmtRow{Lang: lang, Cells: make([]FmtCell, len(RoleOrder))}
		index[lang] = &rows[i]
	}
	setCell := func(lang string, role Role, apply func(*FmtCell)) {
		row, okRow := index[lang]
		if !okRow {
			return
		}
		apply(&row.Cells[int(role)])
	}

	// Mise-capable Format/Lint cells, classified; mise badge derived from config.
	for bare, spec := range classifier {
		for _, rs := range spec.roles {
			ver, isDeclared := declared[bare]
			if isDeclared {
				known[bare] = true
			}
			mason, mver := masonProbe(rs.masonPkg)
			setCell(spec.lang, rs.role, func(c *FmtCell) {
				c.Defined = true
				c.MiseCapable = true
				c.MasonExpects = rs.masonPkg != ""
				c.Tool = rs.display
				c.Mise = isDeclared
				c.MiseVer = ver
				c.Mason = mason
				c.MasonVer = mver
				c.Drift = isDeclared && mason && ver != "" && mver != "" && normalizeVersion(ver) != normalizeVersion(mver)
			})
		}
	}

	// Mason-only supplement cells (Debug/LSP + linters mise doesn't offer).
	for _, s := range supplement {
		mason, mver := masonProbe(s.masonPkg)
		setCell(s.lang, s.role, func(c *FmtCell) {
			c.Defined = true
			c.MiseCapable = false
			c.MasonExpects = s.masonPkg != ""
			c.Tool = s.display
			c.Mason = mason
			c.MasonVer = mver
		})
	}

	overall := OK
	for i := range rows {
		rowStatus := OK
		for j := range rows[i].Cells {
			c := &rows[i].Cells[j]
			if !c.Defined {
				continue
			}
			c.Status = cellStatus(c, tierOn)
			if c.Status > rowStatus {
				rowStatus = c.Status
			}
		}
		rows[i].Status = rowStatus
		if rowStatus > overall {
			overall = rowStatus
		}
	}

	// Any config tool the classifier doesn't recognize — surface, don't drop.
	var unknown []string
	for bare := range declared {
		if !known[bare] {
			unknown = append(unknown, bare)
		}
	}

	return FmtReport{
		TierOn:     tierOn,
		Rows:       rows,
		Status:     overall,
		ConfigPath: path,
		ConfigOK:   ok,
		Unknown:    unknown,
	}
}

// cellStatus judges one defined cell, tier-aware. Mason is the sanctioned backup:
// a dormant mise (tier off) is never itself an error, only a genuine coverage gap
// or a version drift escalates.
func cellStatus(c *FmtCell, tierOn bool) Status {
	if !c.MiseCapable {
		// Debug/LSP-style: Mason is the only possible source.
		if c.Mason {
			return OK
		}
		return Warn
	}
	switch {
	case c.Mise && c.Mason:
		if c.Drift {
			return Warn
		}
		return OK
	case c.Mise && !c.Mason:
		if c.MasonExpects {
			return Warn // editor copy missing while enforcement has it
		}
		// No Mason equivalent exists (Go): enforced copy is the whole story.
		if tierOn {
			return OK
		}
		return Warn // tier off + no editor copy → nothing active
	case !c.Mise && c.Mason:
		if tierOn {
			return Warn // enforcement gap: only the editor has it
		}
		return OK // tier off: Mason is the backup, as designed
	default:
		return Fail // present in neither stack
	}
}

// tierActive reports whether the mise fmt tier is on (MU_MISE_FMT set, or MISE_ENV
// naming the fmt env) — the same opt-in the shell exports.
func tierActive() bool {
	if os.Getenv("MU_MISE_FMT") != "" {
		return true
	}
	for _, e := range strings.Split(os.Getenv("MISE_ENV"), ",") {
		if strings.TrimSpace(e) == "fmt" {
			return true
		}
	}
	return false
}

// fmtConfigPath resolves the user OVERRIDE path — ~/.config/mu/config.fmt.toml (XDG),
// or MU_MISE_FMT_CONFIG when set (explicit path, also the test hook). The file need
// not exist; when it doesn't, the embedded default is used instead.
func fmtConfigPath() string {
	if p := os.Getenv("MU_MISE_FMT_CONFIG"); p != "" {
		return p
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, _ := os.UserHomeDir()
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "mu", "config.fmt.toml")
}

// effectiveFmtConfig returns the declared-tool TOML actually in force and a label for
// its source: the user override when it exists (fully replacing the default), else the
// embedded built-in. Self-contained — never depends on an external file existing.
func effectiveFmtConfig() (data []byte, path string) {
	p := fmtConfigPath()
	if b, err := os.ReadFile(p); err == nil {
		return b, p
	}
	return defaultFmtConfig, "(built-in default)"
}

// EffectiveFmtConfig returns the raw TOML of the declared-tool set in force (user
// override if present, else the embedded default) — for `mu doctor fmt --dump-config`.
func EffectiveFmtConfig() []byte {
	data, _ := effectiveFmtConfig()
	return data
}

// parseFmtConfigBytes reads the [tools] table from TOML bytes, returning bare tool
// name → declared version. Backend prefixes (`pipx:`) and any `@version` request
// suffix are stripped from the key; the value's version string is kept for drift.
func parseFmtConfigBytes(data []byte) (map[string]string, bool) {
	var doc struct {
		Tools map[string]any `toml:"tools"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	out := map[string]string{}
	for key, val := range doc.Tools {
		bare := bareToolName(key)
		out[bare] = toolVersion(val)
	}
	return out, true
}

// bareToolName strips a backend prefix ("pipx:fprettify" → "fprettify") and any
// version request suffix ("ruff@0.15" → "ruff").
func bareToolName(key string) string {
	if i := strings.LastIndex(key, ":"); i >= 0 {
		key = key[i+1:]
	}
	if i := strings.Index(key, "@"); i >= 0 {
		key = key[:i]
	}
	return key
}

// toolVersion extracts the version from a [tools] value, which is either a bare
// version string or a table with a `version` key.
func toolVersion(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]any:
		if s, ok := v["version"].(string); ok {
			return s
		}
	}
	return ""
}

// masonProbe reports whether a Mason package is installed and its version (from
// the receipt's `pkg:...@<version>` id). Empty masonPkg → no equivalent.
func masonProbe(pkg string) (bool, string) {
	if pkg == "" {
		return false, ""
	}
	dir := filepath.Join(masonPackagesDir(), pkg)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false, ""
	}
	return true, masonReceiptVersion(filepath.Join(dir, "mason-receipt.json"))
}

func masonReceiptVersion(receipt string) string {
	data, err := os.ReadFile(receipt)
	if err != nil {
		return ""
	}
	var r struct {
		Source struct {
			ID string `json:"id"`
		} `json:"source"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return ""
	}
	if i := strings.LastIndex(r.Source.ID, "@"); i >= 0 {
		v := r.Source.ID[i+1:]
		if q := strings.IndexAny(v, "?#"); q >= 0 { // drop purl qualifiers (…@0.6.13?extra=YAML)
			v = v[:q]
		}
		return v
	}
	return ""
}

// normalizeVersion canonicalizes a version for drift comparison: mise declares
// "3.13.1" where Mason may report "v3.13.1", so a leading v and any +build suffix
// are stripped. Display keeps the raw strings; only the comparison is normalized.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return v
}

// masonPackagesDir resolves nvim's Mason package dir (XDG_DATA_HOME), overridable
// for tests via MU_MASON_DIR.
func masonPackagesDir() string {
	if d := os.Getenv("MU_MASON_DIR"); d != "" {
		return d
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "nvim", "mason", "packages")
}
