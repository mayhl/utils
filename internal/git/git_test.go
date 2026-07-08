package git

import "testing"

// TestClassifyWip covers the reviewed selection: oldest-first untag, keep beyond N,
// clean for already-untagged, and the n<=0 / n>tagged caps.
func TestClassifyWip(t *testing.T) {
	const u = unreviewed
	lines := []string{
		"h1\t" + u + "first",
		"h2\t" + u + "second",
		"h3\tclean already", // untagged (mid-stack)
		"h4\t" + u + "third",
	}
	acts := func(rows []ReviewRow) []string {
		a := make([]string, len(rows))
		for i, r := range rows {
			a[i] = r.Act
		}
		return a
	}
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// n<=0 → every [unreviewed] untagged; the clean one stays clean.
	rows, tagged, untag := classifyWip(lines, 0)
	if tagged != 3 || untag != 3 {
		t.Errorf("all: tagged=%d untag=%d, want 3/3", tagged, untag)
	}
	if want := []string{"untag", "untag", "clean", "untag"}; !eq(acts(rows), want) {
		t.Errorf("all acts = %v, want %v", acts(rows), want)
	}

	// n=1 → only the OLDEST [unreviewed] untags; the rest keep.
	rows, _, untag = classifyWip(lines, 1)
	if untag != 1 {
		t.Errorf("n=1 untag=%d, want 1", untag)
	}
	if want := []string{"untag", "keep", "clean", "keep"}; !eq(acts(rows), want) {
		t.Errorf("n=1 acts = %v, want %v", acts(rows), want)
	}

	// n greater than the tagged count is capped at all.
	if _, _, untag = classifyWip(lines, 99); untag != 3 {
		t.Errorf("n=99 untag=%d, want 3 (capped)", untag)
	}

	// No tagged commits → nothing to untag.
	rows, tagged, untag = classifyWip([]string{"a\tplain commit"}, 0)
	if tagged != 0 || untag != 0 || rows[0].Act != "clean" {
		t.Errorf("clean-only: tagged=%d untag=%d act=%s", tagged, untag, rows[0].Act)
	}
}
