package queue

import "testing"

// The real six-line banner layout (dividers, then the fiscal-year line framed by blank
// lines — one before it, one before the table); header + ruler verbatim from `show_usage`.
const showUsageSample = `===============================================================
  Allocation usage for tester — updated nightly.
===============================================================

Hours Remaining in the Fiscal Year:  1985 (33.70%)

                               Hours     Hours       Hours    Percent Background
System        Subproject     Allocated    Used     Remaining   Remain Hours Used
========== ================ ========== ========== ========== ======== ==========
hpc1       ABC123DEF           1000000     400000     600000   60.00%          0
hpc1       XYZ789GHI            500000     450000      50000   10.00%      12345
`

func TestParseShowUsage(t *testing.T) {
	got := ParseShowUsage(showUsageSample)
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(got), got)
	}
	want := UsageInfo{
		System: "hpc1", Subproject: "ABC123DEF",
		Allocated: "1000000", Used: "400000", Remaining: "600000",
		PctRemain: "60.00%", Background: "0",
	}
	if got[0] != want {
		t.Errorf("row0:\n got  %+v\n want %+v", got[0], want)
	}
	if got[1].PctRemain != "10.00%" || got[1].Background != "12345" {
		t.Errorf("row1 mismatch: %+v", got[1])
	}
}

func TestParseFiscalYearLeft(t *testing.T) {
	if got := ParseFiscalYearLeft(showUsageSample); got != "33.70" {
		t.Errorf("year left = %q, want 33.70", got)
	}
	// The real line, verbatim in form from a live system; the scan is position-
	// agnostic, so it parses even when a system prints it below the table.
	footer := "System        Subproject     ...\nhpc1  ABC  1  2  3  50.00%  0\n\n" +
		"Hours Remaining in the Fiscal Year:  1985 (22.67%)\n"
	if got := ParseFiscalYearLeft(footer); got != "22.67" {
		t.Errorf("below-table position = %q, want 22.67", got)
	}
	// Wordier paren variants some systems print must still match.
	if got := ParseFiscalYearLeft("banner\nFY ends 30 Sep (41.2% of year remains), plan ahead\n"); got != "41.2" {
		t.Errorf("wordy variant = %q, want 41.2", got)
	}
	// No parenthesized percent → "" (pace column degrades away); bare prose
	// percents must not match.
	if got := ParseFiscalYearLeft("some banner\n100% pure prose here\ntable\n"); got != "" {
		t.Errorf("want no match, got %q", got)
	}
}
