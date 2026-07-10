package cli

import (
	"testing"

	"github.com/mayhl/mayhl_utils/internal/hooks"
)

func TestParseHookLines(t *testing.T) {
	raw := "login banner noise\n" +
		`{"job":"4501","hook":"progress","exit":0,"data":{"pct":38,"eta":"17:20"}}` + "\n" +
		`{"job":"4501","hook":"energy","exit":0,"data":{"kE":"3.2e8"}}` + "\n" +
		`{"job":"4502","hook":"progress","exit":1,"err":"timeout"}` + "\n" +
		"not json {\n"
	m := ParseHookLines(raw)
	if len(m) != 2 || len(m["4501"]) != 2 || len(m["4502"]) != 1 {
		t.Fatalf("parse: %+v", m)
	}
	if got := hookProgress(m["4501"]); got != "38%" {
		t.Errorf("progress: %q", got)
	}
	// a failed progress hook carries no data → empty cell, not an error
	if got := hookProgress(m["4502"]); got != "" {
		t.Errorf("failed hook progress: %q", got)
	}
}

func TestOrderedModel(t *testing.T) {
	rs := []hooks.Result{
		{Hook: "energy", Data: map[string]any{"kE": "3.2e8", "dissip": "low"}},
		{Hook: "progress", Data: map[string]any{"eta": "17:20", "pct": float64(38)}},
	}
	got := orderedModel(rs)
	want := [][2]string{{"pct", "38"}, {"eta", "17:20"}, {"dissip", "low"}, {"kE", "3.2e8"}}
	if len(got) != len(want) {
		t.Fatalf("len: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pair %d: %v want %v", i, got[i], want[i])
		}
	}
}
