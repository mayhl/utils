package render

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// EditorNode is one row of the Editor's tree: a SECTION when it has Children and no Field,
// a LEAF when it carries a Field. Domain-free — the caller builds the tree and reads the
// Changes back out; the widget knows nothing about config.
type EditorNode struct {
	Label  string
	Field  *FormField // nil → a section (a heading you expand), else an editable leaf
	Origin string     // dim provenance note ("from dsrc1", "unset", …); leaves only
	// Hue is the palette hue of a SECTION's heading (HueLoc, HueGroup, …), letting the
	// caller tier its own hierarchy without the widget knowing what a cluster is. "" → the
	// house header hue. Warm hues are status-reserved — don't pass them.
	Hue      string
	Children []EditorNode
}

// EditorSpec configures the panel: a titled tree whose leaves are edited in place with the
// same field editors as Form.
type EditorSpec struct {
	Title string
	Root  []EditorNode
	// Load, when set, runs ONCE off the UI loop at open (it may ssh) and its patches apply
	// when they land — the panel is usable immediately and enriches later, as in Form.
	Load     func() []EditorPatch
	LoadNote string // dim footer notice while Load is in flight
}

// EditorPatch is a late update to one leaf, addressed by its path (not an index — a tree's
// leaf order is an implementation detail the caller shouldn't have to track). Zero parts
// are kept, exactly as FieldPatch.
type EditorPatch struct {
	Path     []string
	Options  []string
	Value    string
	Hint     string
	Validate func(value string, all []string) string
}

// Change is one leaf the user actually edited: its path from the root, and the values on
// either side of the edit. Unchanged leaves are never reported.
type Change struct {
	Path     []string
	Old, New string
}

// Editor runs the interactive panel and returns the changed leaves and whether the user
// saved (false = cancelled). Like Select and Form it is a SELECTOR, not an actuator: it
// returns an intent and never touches disk — the caller renders the diff, confirms, and
// writes. Save is refused while any leaf is invalid (the cursor jumps to the offender).
func Editor(spec EditorSpec) ([]Change, bool, error) {
	res, err := tea.NewProgram(newEditorModel(spec)).Run()
	if err != nil {
		return nil, false, err
	}
	m := res.(editorModel)
	if !m.saved {
		return nil, false, nil
	}
	return m.changes(), true, nil
}

// edRow is the tree flattened once at open: rendering and navigation both walk this slice,
// with expanded gating which rows are visible. A tree that never changes shape doesn't need
// to be re-walked.
type edRow struct {
	label    string
	depth    int
	path     []string
	field    *FormField // nil → section
	origin   string
	hue      string
	orig     string // value at open, for the Change diff
	err      string
	expanded bool
	kids     int // number of DESCENDANT rows (the whole subtree), for collapse skipping
}

type editorModel struct {
	spec    EditorSpec
	rows    []edRow
	cursor  int // index into rows (always a visible row)
	loading bool
	saved   bool
}

func newEditorModel(spec EditorSpec) editorModel {
	m := editorModel{spec: spec, loading: spec.Load != nil}
	m.rows = flatten(spec.Root, 0, nil)
	for i := range m.rows {
		r := &m.rows[i]
		if f := r.field; f != nil {
			if f.Kind == FieldEnum && f.Value == "" && len(f.Options) > 0 {
				f.Value = f.Options[0]
			}
			r.orig = f.Value
		}
	}
	m.validateAll()
	m.cursor = m.firstVisible()
	return m
}

// flatten walks the tree depth-first into rows, recording each section's descendant count
// so a collapsed section can be skipped in one jump.
func flatten(nodes []EditorNode, depth int, prefix []string) []edRow {
	var out []edRow
	for _, n := range nodes {
		path := append(append([]string(nil), prefix...), n.Label)
		row := edRow{label: n.Label, depth: depth, path: path, origin: n.Origin, hue: n.Hue, expanded: true}
		if n.Field != nil {
			f := *n.Field // copy: the widget edits its own state, never the caller's spec
			row.field = &f
		}
		at := len(out)
		out = append(out, row)
		kids := flatten(n.Children, depth+1, path)
		out = append(out, kids...)
		out[at].kids = len(kids)
	}
	return out
}

// editorPatchMsg carries Load's patches back onto the UI loop.
type editorPatchMsg struct{ patches []EditorPatch }

func (m editorModel) Init() tea.Cmd {
	if m.spec.Load == nil {
		return nil
	}
	load := m.spec.Load
	return func() tea.Msg { return editorPatchMsg{load()} }
}

func (m editorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case editorPatchMsg:
		m.loading = false
		for _, p := range msg.patches {
			m.apply(p)
		}
		m.validateAll()
		return m, nil
	case tea.KeyPressMsg:
		switch key := msg.String(); key {
		case "ctrl+c", "esc":
			m.saved = false
			return m, tea.Quit
		case "ctrl+s":
			return m.save()
		case "up", "shift+tab":
			m.move(-1)
		case "down", "tab":
			m.move(1)
		case "enter", "right", "left", " ", "space":
			// On a section these fold; on a leaf the same keys belong to the field editor
			// (an enum cycles with ←/→/space), so only sections consume them.
			if r := &m.rows[m.cursor]; r.field == nil {
				switch key {
				case "right":
					r.expanded = true
				case "left":
					r.expanded = false
				default: // enter/space toggle
					r.expanded = !r.expanded
				}
				return m, nil
			}
			m.edit(msg)
		default:
			if m.rows[m.cursor].field != nil {
				m.edit(msg)
			}
		}
	}
	return m, nil
}

// move steps the cursor to the next visible row in a direction, skipping the subtrees of
// collapsed sections.
func (m *editorModel) move(dir int) {
	for i := m.cursor + dir; i >= 0 && i < len(m.rows); i += dir {
		if m.visible(i) {
			m.cursor = i
			return
		}
	}
}

// visible reports whether row i is shown — i.e. no ancestor of it is collapsed. Walking
// back over the recorded subtree sizes answers this without a parent pointer.
func (m editorModel) visible(i int) bool {
	for j := 0; j < i; j++ {
		if !m.rows[j].expanded && j+m.rows[j].kids >= i {
			return false
		}
	}
	return true
}

func (m editorModel) firstVisible() int {
	for i := range m.rows {
		if m.visible(i) {
			return i
		}
	}
	return 0
}

// save refuses while any leaf is invalid, expanding whatever hides the first offender and
// putting the cursor on it — an error the user can't see is an error they can't fix.
func (m editorModel) save() (tea.Model, tea.Cmd) {
	m.validateAll()
	for i, r := range m.rows {
		if r.err == "" {
			continue
		}
		for j := 0; j < i; j++ {
			if j+m.rows[j].kids >= i {
				m.rows[j].expanded = true
			}
		}
		m.cursor = i
		return m, nil
	}
	m.saved = true
	return m, tea.Quit
}

// edit applies a keystroke to the cursor leaf — the same grammar as Form: enums cycle on
// ←/→/space, text takes runes and backspace.
func (m *editorModel) edit(msg tea.KeyPressMsg) {
	f := m.rows[m.cursor].field
	switch f.Kind {
	case FieldEnum:
		switch msg.String() {
		case "right", " ", "space":
			f.Value = cycleOption(f, 1)
		case "left":
			f.Value = cycleOption(f, -1)
		default:
			return
		}
	case FieldText:
		switch msg.String() {
		case "backspace":
			if r := []rune(f.Value); len(r) > 0 {
				f.Value = string(r[:len(r)-1])
			}
		default:
			if t := msg.Key().Text; t != "" {
				f.Value += t
			} else {
				return
			}
		}
	}
	m.validate(m.cursor)
}

// apply merges a patch into the leaf at its path (zero parts keep the current value). A
// patched Value must be reachable in the options, as in Form — a stale Load can't select
// something the user can no longer cycle to.
func (m *editorModel) apply(p EditorPatch) {
	for i := range m.rows {
		r := &m.rows[i]
		if r.field == nil || !samePath(r.path, p.Path) {
			continue
		}
		f := r.field
		if p.Options != nil {
			f.Options = p.Options
		}
		if p.Hint != "" {
			f.Hint = p.Hint
		}
		if p.Validate != nil {
			f.Validate = p.Validate
		}
		if p.Value != "" && (f.Kind != FieldEnum || contains(f.Options, p.Value)) {
			f.Value = p.Value
			// A value that arrives from Load is the file's truth, not an edit — otherwise
			// every async fill would show up as a pending change.
			r.orig = p.Value
		}
		return
	}
}

func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m *editorModel) validate(i int) {
	r := &m.rows[i]
	if r.field == nil || r.field.Validate == nil {
		r.err = ""
		return
	}
	r.err = r.field.Validate(r.field.Value, m.leafValues())
}

func (m *editorModel) validateAll() {
	for i := range m.rows {
		m.validate(i)
	}
}

// leafValues is every leaf's value in row order — the `all` a FormField.Validate closure
// sees, so a cross-field rule can reach its neighbours.
func (m editorModel) leafValues() []string {
	var out []string
	for _, r := range m.rows {
		if r.field != nil {
			out = append(out, r.field.Value)
		}
	}
	return out
}

// changes reports the leaves whose value actually moved.
func (m editorModel) changes() []Change {
	var out []Change
	for _, r := range m.rows {
		if r.field != nil && r.field.Value != r.orig {
			out = append(out, Change{Path: r.path, Old: r.orig, New: r.field.Value})
		}
	}
	return out
}

func (m editorModel) View() tea.View {
	labelW := 0
	for i, r := range m.rows {
		if r.field != nil && m.visible(i) {
			labelW = max(labelW, len(r.label)+2*r.depth)
		}
	}
	var lines []string
	for i, r := range m.rows {
		if !m.visible(i) {
			continue
		}
		cur := i == m.cursor
		indent := strings.Repeat("  ", r.depth)
		if r.field == nil {
			fold := glyph("▾ ", "- ")
			if !r.expanded {
				fold = glyph("▸ ", "+ ")
			}
			head := selHeader
			if r.hue != "" {
				head = lg(r.hue).Bold(true)
			}
			lines = append(lines, cursorGlyph(cur)+" "+indent+head.Render(fold+r.label))
			continue
		}
		// An UNSET leaf is dim and shows a dash: an empty cell and a real empty string look
		// identical otherwise, and here the difference decides whether a key gets written.
		val, style := r.field.Value, edValue
		if strings.TrimSpace(val) == "" && !cur {
			val, style = glyph("—", "--"), edUnset
		}
		switch {
		case r.field.Kind == FieldEnum && cur:
			val = glyph("‹ ", "< ") + val + glyph(" ›", " >")
		case cur:
			val += glyph("▏", "|")
		}
		if strings.TrimSpace(val) == "" {
			val = " "
		}
		line := cursorGlyph(cur) + " " + edKey.Render(padRight(indent+r.label, labelW)) + "  "
		if cur {
			line += selRow.Render(val)
		} else {
			line += style.Render(val)
		}
		// Provenance is why this panel beats the file: which scope a value came from is
		// invisible in the TOML but decides what a lookup resolves to. The caller writes the
		// note (and any marker glyph) — meaning rides the glyph, not a colour, so this stays
		// dim chrome either way.
		if r.origin != "" {
			line += "  " + selFoot.Render(r.origin)
		}
		if r.field.Hint != "" {
			line += "  " + selFoot.Render(r.field.Hint)
		}
		lines = append(lines, line)
		if r.err != "" {
			lines = append(lines, "  "+formErr.Render(glyph("✗ ", "x ")+r.err))
		}
	}

	box := selBox
	if asciiMode() {
		box = box.Border(asciiBorder)
	}
	dot := glyph(" · ", " - ")
	foot := glyph("↑↓", "u/d") + " move" + dot + "type to edit" + dot +
		glyph("←→", "l/r") + " fold/cycle" + dot + "ctrl+s save" + dot + "esc cancel"
	if m.loading && m.spec.LoadNote != "" {
		foot += dot + m.spec.LoadNote
	}
	out := selTitle.Render(m.spec.Title) + "\n" + box.Render(strings.Join(lines, "\n")) +
		"\n" + selFoot.Render(foot)
	return tea.NewView(out)
}

var (
	edKey   = lg(HueID)   // cyan key labels, matching Form's field labels
	edValue = lg(HueName) // white — the value is the content, so it carries the plain hue
	edUnset = lg(HueDim)  // a key the file doesn't set
)
