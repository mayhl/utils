package shell

import "testing"

func TestQuote(t *testing.T) {
	if got := Quote("plain"); got != "'plain'" {
		t.Errorf("plain: %q", got)
	}
	if got := Quote("a'b"); got != `'a'\''b'` {
		t.Errorf("embedded quote: %q", got)
	}
}
