package hpc

import (
	"testing"
	"time"
)

func TestParseKlist(t *testing.T) {
	out := `Ticket cache: API:4502F01E-1FFC-4C6A-AB96-5F866F1ECE00
Default principal: mayhl@REALM.EXAMPLE

Valid starting       Expires              Service principal
07/04/2026 11:44:12  07/04/2026 21:44:12  krbtgt/REALM.EXAMPLE@REALM.EXAMPLE
07/04/2026 11:44:47  07/04/2026 21:44:12  host/node2.beta.example.mil@
`
	info := parseKlist(out)
	if info.Principal != "mayhl@REALM.EXAMPLE" {
		t.Errorf("principal = %q", info.Principal)
	}
	want := time.Date(2026, 7, 4, 21, 44, 12, 0, time.Local)
	if !info.Expires.Equal(want) {
		t.Errorf("expires = %v, want %v", info.Expires, want)
	}
}

func TestParseKlistNoTGT(t *testing.T) {
	info := parseKlist("Default principal: bob@REALM\n(nothing else)\n")
	if info.Principal != "bob@REALM" {
		t.Errorf("principal = %q", info.Principal)
	}
	if !info.Expires.IsZero() {
		t.Errorf("expires should be zero when no TGT line, got %v", info.Expires)
	}
}
