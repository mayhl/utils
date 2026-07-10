package render

import "testing"

// asciiMode is the single ASCII gate for all house output — status lines, tables,
// progress bars, and the picker. It trips on MU_ASCII or an explicitly non-UTF-8 locale;
// an unset locale is treated as UTF-8-capable, and LC_ALL/LC_CTYPE win over LANG.
func TestAsciiMode(t *testing.T) {
	cases := []struct {
		name                  string
		muAscii, lcAll, lcCty string
		lang                  string
		want                  bool
	}{
		{name: "all unset → UTF-8", want: false},
		{name: "MU_ASCII forces ascii", muAscii: "1", lang: "en_US.UTF-8", want: true},
		{name: "explicit C locale", lang: "C", want: true},
		{name: "latin1 locale", lang: "en_US.ISO-8859-1", want: true},
		{name: "utf-8 lang", lang: "en_US.UTF-8", want: false},
		{name: "utf8 no-dash spelling", lang: "en_US.utf8", want: false},
		{name: "LC_ALL C wins over UTF-8 LANG", lcAll: "C", lang: "en_US.UTF-8", want: true},
		{name: "LC_CTYPE UTF-8 wins over C LANG", lcCty: "en_US.UTF-8", lang: "C", want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MU_ASCII", c.muAscii)
			t.Setenv("LC_ALL", c.lcAll)
			t.Setenv("LC_CTYPE", c.lcCty)
			t.Setenv("LANG", c.lang)
			if got := asciiMode(); got != c.want {
				t.Errorf("asciiMode() = %v, want %v", got, c.want)
			}
		})
	}
}

// glyph routes through asciiMode, so a non-UTF-8 locale (not just MU_ASCII) now selects
// the ASCII form — the consolidation's whole point for static output.
func TestGlyphHonorsLocale(t *testing.T) {
	t.Setenv("MU_ASCII", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")

	t.Setenv("LANG", "en_US.UTF-8")
	if got := glyph("✓", "OK"); got != "✓" {
		t.Errorf("UTF-8 locale: glyph = %q, want ✓", got)
	}
	t.Setenv("LANG", "C")
	if got := glyph("✓", "OK"); got != "OK" {
		t.Errorf("C locale: glyph = %q, want OK", got)
	}
}
