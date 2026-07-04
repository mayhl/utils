package rsync

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestRunLocalTransfer drives the full non-verbose path (progress2 + -v + --stats
// parsing → summary) through a real local rsync, verifying the wiring and that
// files actually transfer.
func TestRunLocalTransfer(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.bin"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "b.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := Run([]string{"-a", src + "/", dst + "/"}, "test-copy", false); rc != 0 {
		t.Fatalf("Run rc=%d", rc)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.bin")); err != nil {
		t.Errorf("a.bin not copied: %v", err)
	}
}

// TestProgressRE checks the --info=progress2 parser against real rsync 3.x
// output, including the final line's trailing "(xfr#…, to-chk=…)" suffix.
func TestProgressRE(t *testing.T) {
	cases := []struct {
		line           string
		pct, rate, eta string
		wantMatch      bool
	}{
		{"  1,234,567  45%  12.34MB/s    0:00:12", "45", "12.34MB/s", "0:00:12", true},
		{"     90,000,000 100%  277.67MB/s    0:00:00 (xfr#1, to-chk=0/1)", "100", "277.67MB/s", "0:00:00", true},
		{"sending incremental file list", "", "", "", false},
	}
	for _, c := range cases {
		m := progressRE.FindStringSubmatch(c.line)
		if (m != nil) != c.wantMatch {
			t.Fatalf("match=%v want %v for %q", m != nil, c.wantMatch, c.line)
		}
		if !c.wantMatch {
			continue
		}
		if m[2] != c.pct || m[3] != c.rate || m[4] != c.eta {
			t.Errorf("parsed (%s,%s,%s) want (%s,%s,%s)", m[2], m[3], m[4], c.pct, c.rate, c.eta)
		}
	}
}

// TestScanLinesCR verifies that a \r-overwritten progress stream splits into
// individual tokens (rsync uses \r, not \n, between progress updates).
func TestScanLinesCR(t *testing.T) {
	stream := "sending list\n 10%\r 55%\r100%\ndone"
	sc := bufio.NewScanner(strings.NewReader(stream))
	sc.Split(scanLinesCR)
	var got []string
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	want := []string{"sending list", " 10%", " 55%", "100%", "done"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestCrackProgress(t *testing.T) {
	cases := []struct {
		in          []string
		kept, strip []string
	}{
		{[]string{"-avuP"}, []string{"-au"}, []string{"-v", "-P"}},
		{[]string{"-au", "--partial"}, []string{"-au", "--partial"}, nil},
		{[]string{"--verbose", "--info=progress2", "-z"}, []string{"-z"}, []string{"--verbose", "--info=progress2"}},
		{[]string{"-vP"}, nil, []string{"-v", "-P"}},
	}
	for _, c := range cases {
		kept, strip := crackProgress(c.in)
		if !reflect.DeepEqual(kept, c.kept) || !reflect.DeepEqual(strip, c.strip) {
			t.Errorf("crackProgress(%q) = (%q,%q) want (%q,%q)", c.in, kept, strip, c.kept, c.strip)
		}
	}
}

func TestShellSplit(t *testing.T) {
	got := shellSplit("-au --partial --exclude '*.tmp'")
	want := []string{"-au", "--partial", "--exclude", "*.tmp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shellSplit = %q want %q", got, want)
	}
}

func TestStatsSummary(t *testing.T) {
	stats := []string{
		"Number of files: 12 (reg: 10, dir: 2)",
		"Number of regular files transferred: 3",
		"Total file size: 4,509,715,660 bytes",
		"sent 1,354,000,000 bytes  received 4,210 bytes  47,314,000.00 bytes/sec",
		"total size is 4,509,715,660  speedup is 3.11",
	}
	got := statsSummary(stats)
	want := []string{"3/10 files (7 unchanged)", "4.2GB", "45.12MB/s", "3.11× speedup"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("statsSummary =\n %q\nwant %q", got, want)
	}
	if p := statsSummary(nil); len(p) != 0 {
		t.Errorf("empty stats should yield no parts, got %q", p)
	}
}

func TestFileLine(t *testing.T) {
	drop := []string{"", "./", "sub/", "sending incremental file list", "receiving incremental file list"}
	for _, d := range drop {
		if fileLine(d) != "" {
			t.Errorf("fileLine(%q) should be empty", d)
		}
	}
	if fileLine("data/big.bin") != "data/big.bin" {
		t.Errorf("fileLine dropped a real path")
	}
}

func TestCanonKeys(t *testing.T) {
	keys := canonKeys([]string{"-z", "--timeout=30", "--exclude", "foo"})
	if !keys["--compress"] || !keys["--timeout"] {
		t.Errorf("missing expected keys: %v", keys)
	}
	if keys["--exclude"] {
		t.Errorf("--exclude should not be a canonical dup key")
	}
}
