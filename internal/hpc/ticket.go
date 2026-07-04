package hpc

import (
	"os/exec"
	"regexp"
	"time"
)

// TicketInfo is the parsed state of the local Kerberos credential cache.
type TicketInfo struct {
	Present   bool
	Principal string    // e.g. "mayhl@REALM.EXAMPLE"
	Expires   time.Time // TGT expiry (zero if unparsed)
}

var (
	reKlistPrincipal = regexp.MustCompile(`Default principal:\s+(\S+)`)
	// The TGT (krbtgt/…) row: "MM/DD/YYYY HH:MM:SS  MM/DD/YYYY HH:MM:SS  krbtgt/…".
	// The second timestamp is the ticket's expiry.
	reKlistTGT = regexp.MustCompile(`(?m)^\s*\d\d/\d\d/\d{4} \d\d:\d\d:\d\d\s+(\d\d/\d\d/\d{4} \d\d:\d\d:\d\d)\s+krbtgt/`)
)

// parseKlist extracts the principal and TGT expiry from MIT klist output. Pure
// (no clock) so it's testable; the caller compares Expires against now.
func parseKlist(out string) TicketInfo {
	var info TicketInfo
	if m := reKlistPrincipal.FindStringSubmatch(out); m != nil {
		info.Principal = m[1]
	}
	if m := reKlistTGT.FindStringSubmatch(out); m != nil {
		if t, err := time.ParseInLocation("01/02/2006 15:04:05", m[1], time.Local); err == nil {
			info.Expires = t
		}
	}
	return info
}

// Ticket runs klist and returns the parsed credential state. The second return is
// false when klist isn't installed (no local Kerberos here). A non-zero klist exit
// means no cached ticket (Present=false).
func Ticket() (TicketInfo, bool) {
	klist, err := exec.LookPath("klist")
	if err != nil {
		return TicketInfo{}, false
	}
	out, err := exec.Command(klist).CombinedOutput()
	if err != nil {
		return TicketInfo{Present: false}, true
	}
	info := parseKlist(string(out))
	info.Present = true
	return info, true
}
