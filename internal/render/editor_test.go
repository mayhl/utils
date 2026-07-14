package render

import "testing"

// tree builds a two-cluster spec: sections nesting sections nesting leaves — the shape
// config actually has (cluster → node → keys).
func tree() EditorSpec {
	leaf := func(label, val string) EditorNode {
		return EditorNode{Label: label, Field: &FormField{Label: label, Value: val}}
	}
	return EditorSpec{
		Title: "config",
		Root: []EditorNode{
			leaf("hpc_user", "someuser"),
			{Label: "dsrc1", Children: []EditorNode{
				leaf("scheduler", "pbs"),
				{Label: "node-a", Children: []EditorNode{
					{Label: "account", Field: &FormField{Label: "account", Value: "ALLOC-1"}, Origin: "cluster default"},
				}},
			}},
		},
	}
}

// TestFlattenAndVisible covers the navigation model: the tree flattens once, and a
// collapsed section hides its WHOLE subtree (not just its immediate children).
func TestFlattenAndVisible(t *testing.T) {
	m := newEditorModel(tree())
	if len(m.rows) != 5 { // hpc_user, dsrc1, scheduler, node-a, account
		t.Fatalf("rows = %d, want 5", len(m.rows))
	}
	// dsrc1 (row 1) owns everything below it.
	if got := m.rows[1].kids; got != 3 {
		t.Errorf("dsrc1 subtree = %d rows, want 3", got)
	}
	for i := range m.rows {
		if !m.visible(i) {
			t.Errorf("row %d hidden while everything is expanded", i)
		}
	}
	// Collapsing dsrc1 must hide the nested node-a AND its leaf, two levels down.
	m.rows[1].expanded = false
	for _, i := range []int{2, 3, 4} {
		if m.visible(i) {
			t.Errorf("row %d visible under a collapsed dsrc1", i)
		}
	}
	if !m.visible(0) || !m.visible(1) {
		t.Error("collapsing dsrc1 hid itself or its sibling")
	}
	// Moving down from hpc_user must SKIP the collapsed subtree, not land inside it.
	m.cursor = 0
	m.move(1)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want dsrc1 (1)", m.cursor)
	}
	m.move(1)
	if m.cursor != 1 {
		t.Errorf("cursor = %d — moved into a collapsed subtree", m.cursor)
	}
}

// TestChanges is the widget's contract: it reports the leaves that moved, by path, and
// stays silent about the rest.
func TestChanges(t *testing.T) {
	m := newEditorModel(tree())
	if got := m.changes(); len(got) != 0 {
		t.Errorf("untouched panel reports %d changes", len(got))
	}
	m.rows[4].field.Value = "ALLOC-9" // the nested account leaf

	got := m.changes()
	if len(got) != 1 {
		t.Fatalf("changes = %d, want 1", len(got))
	}
	c := got[0]
	if c.Old != "ALLOC-1" || c.New != "ALLOC-9" {
		t.Errorf("change = %q → %q", c.Old, c.New)
	}
	if !samePath(c.Path, []string{"dsrc1", "node-a", "account"}) {
		t.Errorf("path = %v, want the full path from the root", c.Path)
	}
}

// TestPatchByPath: a late Load fills a leaf by PATH, and the filled value is the file's
// truth — not a pending edit the user never made.
func TestPatchByPath(t *testing.T) {
	m := newEditorModel(tree())
	m.apply(EditorPatch{
		Path:    []string{"dsrc1", "node-a", "account"},
		Options: []string{"ALLOC-1", "ALLOC-2"},
		Hint:    "from show_usage",
	})
	if got := m.rows[4].field.Hint; got != "from show_usage" {
		t.Errorf("hint = %q — patch missed the leaf", got)
	}
	m.apply(EditorPatch{Path: []string{"hpc_user"}, Value: "filled"})
	if got := m.rows[0].field.Value; got != "filled" {
		t.Errorf("value = %q", got)
	}
	if got := m.changes(); len(got) != 0 {
		t.Errorf("a Load-filled value counted as an edit: %+v", got)
	}
}

// TestSaveBlocksOnInvalid: save must not slip past a bad value, and must surface it —
// including one buried in a collapsed section, which the user otherwise cannot see.
func TestSaveBlocksOnInvalid(t *testing.T) {
	spec := tree()
	spec.Root[1].Children[1].Children[0].Field.Validate = func(v string, _ []string) string {
		if v == "" {
			return "required"
		}
		return ""
	}
	m := newEditorModel(spec)
	m.rows[4].field.Value = ""
	m.rows[1].expanded = false // bury the offender

	res, _ := m.save()
	got := res.(editorModel)
	if got.saved {
		t.Fatal("saved with an invalid leaf")
	}
	if got.cursor != 4 {
		t.Errorf("cursor = %d, want the offending leaf (4)", got.cursor)
	}
	if !got.visible(4) {
		t.Error("the offending leaf is still hidden — an error the user can't see")
	}
}
