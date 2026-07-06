package render

import "testing"

func row(id string, cells ...string) SelectRow { return SelectRow{ID: id, Cells: cells} }

// A live refresh must: pick up new rows, drop gone ones, and keep the ID-keyed
// selection + the cursor's ID across the rebuild.
func TestRefreshKeepsSelectionByID(t *testing.T) {
	rows1 := []SelectRow{row("100", "100", "a"), row("200", "200", "b")}
	rows2 := []SelectRow{row("200", "200", "b"), row("300", "300", "c")} // 100 gone, 300 new
	spec := SelectSpec{Verb: "kill", Columns: []string{"ID", "CMD"}, Fetch: func() []SelectRow { return rows2 }}
	m := newSelectModel(spec, rows1)

	m.selected["200"] = true
	m.cursor = 1 // on ID 200

	m.refresh()

	if len(m.rows) != 2 || m.rows[0].ID != "200" {
		t.Fatalf("refresh didn't swap in the new snapshot: %+v", m.rows)
	}
	has := func(id string) bool {
		for _, r := range m.rows {
			if r.ID == id {
				return true
			}
		}
		return false
	}
	if has("100") {
		t.Error("gone row 100 still present after refresh")
	}
	if !has("300") {
		t.Error("new row 300 missing after refresh")
	}
	if !m.selected["200"] {
		t.Error("selection for ID 200 lost across refresh")
	}
	if got := m.rows[m.visible[m.cursor]].ID; got != "200" {
		t.Errorf("cursor didn't follow ID 200, landed on %s", got)
	}
}

// refresh must respect an active filter (e.g. resumed after the filter box closes).
func TestRefreshHonorsFilter(t *testing.T) {
	rows := []SelectRow{row("1", "1", "funwave"), row("2", "2", "python")}
	spec := SelectSpec{Verb: "kill", Columns: []string{"ID", "CMD"}, Fetch: func() []SelectRow { return rows }}
	m := newSelectModel(spec, rows)
	m.filter = "python"
	m.recompute()
	if len(m.visible) != 1 || m.rows[m.visible[0]].ID != "2" {
		t.Fatalf("filter not applied: %+v", m.visible)
	}
	m.refresh()
	if len(m.visible) != 1 || m.rows[m.visible[0]].ID != "2" {
		t.Errorf("refresh dropped the active filter: visible=%+v", m.visible)
	}
}
