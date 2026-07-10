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

// TestSignedHash covers the base-anchor predicate: only a 2nd field of exactly "G" is
// signed; "N"/"U"/short lines are not.
func TestSignedHash(t *testing.T) {
	cases := []struct {
		line     string
		wantHash string
		wantOK   bool
	}{
		{"abc123 G", "abc123", true},
		{"abc123 N", "", false},
		{"abc123 U", "", false},
		{"abc123", "", false}, // no signature field
		{"", "", false},
	}
	for _, c := range cases {
		if h, ok := signedHash(c.line); h != c.wantHash || ok != c.wantOK {
			t.Errorf("signedHash(%q) = %q,%v; want %q,%v", c.line, h, ok, c.wantHash, c.wantOK)
		}
	}
}

// TestClassifyPush covers the pushsigned prefix rule: the contiguous signed prefix pushes,
// and the first unsigned commit + everything after it (INCLUDING a later-signed commit)
// is held.
func TestClassifyPush(t *testing.T) {
	acts := func(rows []PushRow) []bool {
		a := make([]bool, len(rows))
		for i, r := range rows {
			a[i] = r.Push
		}
		return a
	}
	// signed, signed, UNSIGNED, signed-again → push,push,held,held
	lines := []string{
		"h1\tG\tfirst",
		"h2\tG\tsecond",
		"h3\tN\tunsigned wip",
		"h4\tG\tsigned but after the break",
	}
	rows, pushN, held := classifyPush(lines)
	if pushN != 2 || held != 2 {
		t.Errorf("pushN=%d held=%d, want 2/2", pushN, held)
	}
	for i, want := range []bool{true, true, false, false} {
		if acts(rows)[i] != want {
			t.Errorf("row %d push=%v, want %v", i, acts(rows)[i], want)
		}
	}
	// All signed → all push.
	if _, p, h := classifyPush([]string{"a\tG\tx", "b\tG\ty"}); p != 2 || h != 0 {
		t.Errorf("all-signed: pushN=%d held=%d, want 2/0", p, h)
	}
	// First commit unsigned → nothing pushes.
	if _, p, h := classifyPush([]string{"a\tN\tx", "b\tG\ty"}); p != 0 || h != 2 {
		t.Errorf("first-unsigned: pushN=%d held=%d, want 0/2", p, h)
	}
}

// TestClassifySign covers the signwip split: [unreviewed] commits are skipped, the rest
// signed; counts reflect total and tagged.
func TestClassifySign(t *testing.T) {
	const u = unreviewed
	lines := []string{
		"h1\tclean subject",
		"h2\t" + u + "wip one",
		"h3\tanother clean",
		"h4\t" + u + "wip two",
	}
	rows, total, tagged := classifySign(lines)
	if total != 4 || tagged != 2 {
		t.Errorf("total=%d tagged=%d, want 4/2", total, tagged)
	}
	for i, want := range []string{"sign", "skip", "sign", "skip"} {
		if rows[i].Act != want {
			t.Errorf("row %d act=%s, want %s", i, rows[i].Act, want)
		}
	}
}

// TestPrefixBase pins the contiguous-signed-prefix walk — notably the SANDWICH case
// (unsigned under signed, from a pinentry-failed sign or a mid-stack [unreviewed]):
// the base must stop BELOW the unsigned commit so it stays in the WIP range.
func TestPrefixBase(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		floor string
		want  string
	}{
		{"all signed", []string{"a G", "b G", "c G"}, "f", "c"},
		{"all unsigned", []string{"a N", "b N"}, "f", "f"},
		{"prefix then wip", []string{"a G", "b G", "c N", "d N"}, "f", "b"},
		{"sandwich stops below", []string{"a G", "b N", "c G", "d G"}, "f", "a"},
		{"no floor, unsigned first", []string{"a N"}, "", ""},
		{"empty range", nil, "f", "f"},
	}
	for _, c := range cases {
		if got := prefixBase(c.lines, c.floor); got != c.want {
			t.Errorf("%s: prefixBase = %q, want %q", c.name, got, c.want)
		}
	}
}
