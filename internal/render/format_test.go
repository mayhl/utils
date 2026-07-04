package render

import "testing"

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{0: "0B", 512: "512B", 1024: "1.0KB", 1048576: "1.0MB", 5_368_709_120: "5.0GB"}
	for n, want := range cases {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestHumanRate(t *testing.T) {
	if got := HumanRate(1536); got != "1.50KB/s" {
		t.Errorf("HumanRate = %q", got)
	}
}

func TestFmtETA(t *testing.T) {
	if got := FmtETA(3672); got != "1:01:12" {
		t.Errorf("FmtETA = %q", got)
	}
	if got := FmtETA(-1); got != "--:--:--" {
		t.Errorf("FmtETA(neg) = %q", got)
	}
}
