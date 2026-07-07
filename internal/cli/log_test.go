package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/mayhl/mayhl_utils/internal/render"
)

// TestLogSelectRows checks the viewer row adaptation: cell layout, tier glyph+hue,
// the blue-time / magenta-scope hues, the raw-stamp fallback, a stable row ID, and the
// leading ⊕ payload marker (2-col slot, so a payload-less row keeps the alignment).
func TestLogSelectRows(t *testing.T) {
	rows := []render.LogRow{
		{Time: time.Date(2026, 7, 6, 9, 12, 0, 0, time.Local), Level: "ERROR", Scope: "job", Msg: "cancelled"},
		{RawTS: "bogus", Level: "OK", Scope: "cp", Msg: "done"}, // zero Time → RawTS shown
		{Time: time.Date(2026, 7, 6, 9, 13, 0, 0, time.Local), Level: "OK", Scope: "sshfs", Msg: "mounted", Payload: `{"id":"abc"}`},
	}
	got := logSelectRows(rows)
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}

	r0 := got[0]
	if r0.Cells[1] != "✗" || r0.Hues[1] != render.HueErr {
		t.Errorf("ERROR glyph/hue = %q/%q, want ✗/%s", r0.Cells[1], r0.Hues[1], render.HueErr)
	}
	if !strings.HasPrefix(r0.Cells[0], "07-06 09:12:00") {
		t.Errorf("time cell = %q, want 07-06 09:12:00…", r0.Cells[0])
	}
	if r0.Cells[2] != "job" || r0.Cells[3] != "  cancelled" { // no payload → 2-space slot
		t.Errorf("scope/msg = %q/%q", r0.Cells[2], r0.Cells[3])
	}
	if r0.Hues[0] != render.HueLoc || r0.Hues[2] != render.HueUser {
		t.Errorf("time/scope hue = %q/%q, want %s/%s", r0.Hues[0], r0.Hues[2], render.HueLoc, render.HueUser)
	}
	if !strings.Contains(r0.ID, "cancelled") {
		t.Errorf("row ID should include the message: %q", r0.ID)
	}

	r1 := got[1] // zero-time OK row → RawTS + green ✓
	if r1.Cells[0] != "bogus" || r1.Cells[1] != "✓" || r1.Hues[1] != render.HueOK {
		t.Errorf("zero-time OK row = cells %v hues %v", r1.Cells, r1.Hues)
	}

	r2 := got[2] // payloaded row → leading ⊕ marker, raw ID unmarked
	if r2.Cells[3] != "⊕ mounted" {
		t.Errorf("payload marker cell = %q, want %q", r2.Cells[3], "⊕ mounted")
	}
	if strings.Contains(r2.ID, "⊕") {
		t.Errorf("row ID should stay unmarked: %q", r2.ID)
	}
}

// TestParseLogLinePayload checks the reader splits the optional tab-delimited payload
// off the message, and leaves a plain line's message intact.
func TestParseLogLinePayload(t *testing.T) {
	e, ok := parseLogLine("[2026-07-07T00:00:00] [OK   ] [cp] done\t{\"id\":\"abc\",\"n\":3}")
	if !ok {
		t.Fatal("parse failed")
	}
	if e.msg != "done" {
		t.Errorf("msg = %q, want %q (payload stripped)", e.msg, "done")
	}
	if !strings.Contains(e.payload, `"id":"abc"`) {
		t.Errorf("payload = %q, want the JSON suffix", e.payload)
	}

	e2, _ := parseLogLine("[2026-07-07T00:00:00] [INFO ] [cp] just a message")
	if e2.payload != "" || e2.msg != "just a message" {
		t.Errorf("no-payload parse = msg %q payload %q", e2.msg, e2.payload)
	}
}
