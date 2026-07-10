package render

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func formKey(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func step(t *testing.T, m formModel, keys ...string) formModel {
	t.Helper()
	for _, k := range keys {
		nm, _ := m.Update(formKey(k))
		m = nm.(formModel)
	}
	return m
}

// TestFormEditSubmit walks the core loop: type into a text field, cycle an enum,
// submit with ctrl+s — values come back in field order.
func TestFormEditSubmit(t *testing.T) {
	m := newFormModel(FormSpec{Fields: []FormField{
		{Label: "name", Kind: FieldText},
		{Label: "queue", Kind: FieldEnum, Options: []string{"standard", "debug"}},
	}})
	m = step(t, m, "w", "v", "tab", "right")
	if got := m.values(); got[0] != "wv" || got[1] != "debug" {
		t.Fatalf("values = %v", got)
	}
	m = step(t, m, "left", "backspace") // enum field: backspace must not eat the enum, left cycles back
	if got := m.values(); got[0] != "wv" || got[1] != "standard" {
		t.Fatalf("after left/backspace: %v", got)
	}
	m = step(t, m, "ctrl+s")
	if !m.confirmed {
		t.Fatal("ctrl+s should submit a valid form")
	}
}

// TestFormValidationBlocksSubmit locks the submit gate: an invalid field refuses
// ctrl+s and pulls the cursor to itself; fixing the value lets enter-on-last submit.
// The Validate closure sees ALL values (cross-field: field 1's rule reads field 0).
func TestFormValidationBlocksSubmit(t *testing.T) {
	m := newFormModel(FormSpec{Fields: []FormField{
		{Label: "queue", Kind: FieldEnum, Options: []string{"debug", "standard"}},
		{Label: "walltime", Kind: FieldText, Value: "99:00:00", Validate: func(v string, all []string) string {
			if all[0] == "debug" && v > "01:00:00" {
				return "over the debug cap"
			}
			return ""
		}},
	}})
	m = step(t, m, "ctrl+s")
	if m.confirmed {
		t.Fatal("submit must be refused while invalid")
	}
	if m.cursor != 1 {
		t.Fatalf("cursor should jump to the invalid field, at %d", m.cursor)
	}
	for range "99:00:00" {
		m = step(t, m, "backspace")
	}
	for _, c := range "00:30:00" {
		m = step(t, m, string(c))
	}
	m = step(t, m, "enter") // last field → submit
	if !m.confirmed {
		t.Fatalf("fixed form should submit; errs=%v values=%v", m.errs, m.values())
	}
}

// TestFormView smokes the render path: labels, values, the validation message, and
// the load notice all land in the frame (content only — layout is eyeballed live).
func TestFormView(t *testing.T) {
	m := newFormModel(FormSpec{
		Title: "Submit to alpha",
		Fields: []FormField{
			{Label: "queue", Kind: FieldEnum, Options: []string{"standard"}},
			{Label: "walltime", Value: "bogus", Validate: func(string, []string) string { return "want HH:MM:SS" }},
		},
		Load:     func() []FieldPatch { return nil },
		LoadNote: "fetching queues...",
	})
	out := m.View().Content
	for _, want := range []string{"Submit to alpha", "queue", "standard", "walltime", "bogus", "want HH:MM:SS", "fetching queues..."} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q:\n%s", want, out)
		}
	}
}

// TestFormPatch locks Load's merge rules: options replace wholesale, a patched
// value only takes when it exists in the new options, zero parts keep the field.
func TestFormPatch(t *testing.T) {
	m := newFormModel(FormSpec{Fields: []FormField{
		{Label: "queue", Kind: FieldEnum, Options: []string{"(default)"}},
		{Label: "account", Kind: FieldText, Value: "PROJ1"},
	}})
	nm, _ := m.Update(formPatchMsg{patches: []FieldPatch{
		{Index: 0, Options: []string{"(default)", "standard", "gpu_short"}, Value: "gpu_short"},
		{Index: 0, Value: "ghost"}, // not in options → ignored
		{Index: 7},                 // out of range → ignored
	}})
	m = nm.(formModel)
	if m.loading { // patches landed → loading cleared
		t.Fatal("loading should clear once patches land")
	}
	if got := m.values(); got[0] != "gpu_short" || got[1] != "PROJ1" {
		t.Fatalf("values after patch = %v", got)
	}
	if len(m.fields[0].Options) != 3 {
		t.Fatalf("options not replaced: %v", m.fields[0].Options)
	}
}
