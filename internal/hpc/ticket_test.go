package hpc

import (
	"testing"
	"time"
)

func TestParseKlist(t *testing.T) {
	out := `Ticket cache: API:4502F01E-1FFC-4C6A-AB96-5F866F1ECE00
Default principal: alice@REALM.EXAMPLE

Valid starting       Expires              Service principal
07/04/2026 11:44:12  07/04/2026 21:44:12  krbtgt/REALM.EXAMPLE@REALM.EXAMPLE
07/04/2026 11:44:47  07/04/2026 21:44:12  host/node.example.mil@
`
	info := parseKlist(out)
	if info.Principal != "alice@REALM.EXAMPLE" {
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

func TestTicketUsable(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	live := TicketInfo{Present: true, Principal: "alice@REALM", Expires: now.Add(8 * time.Hour)}
	cases := []struct {
		name string
		info TicketInfo
		want bool
	}{
		{"live", live, true},
		{"absent", TicketInfo{}, false},
		// The bug that bit: an expired ticket still lists its principal, so
		// presence alone must not pass.
		{"expired", TicketInfo{Present: true, Principal: "alice@REALM", Expires: now.Add(-time.Hour)}, false},
		{"expiring within margin", TicketInfo{Present: true, Principal: "alice@REALM", Expires: now.Add(ticketMargin / 2)}, false},
		{"unparsed expiry trusted", TicketInfo{Present: true, Principal: "alice@REALM"}, true},
		{"someone else's ticket", TicketInfo{Present: true, Principal: "mallory@REALM", Expires: now.Add(8 * time.Hour)}, false},
		{"prefix is not a match", TicketInfo{Present: true, Principal: "alicelong@REALM", Expires: now.Add(8 * time.Hour)}, false},
	}
	for _, c := range cases {
		if got := ticketUsable(c.info, "alice", now); got != c.want {
			t.Errorf("%s: ticketUsable = %v, want %v", c.name, got, c.want)
		}
	}
}
