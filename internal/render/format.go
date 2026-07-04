package render

import (
	"fmt"
	"math"
)

// HumanBytes formats a byte count as a compact size (e.g. 1536 → "1.5KB").
func HumanBytes(n int64) string {
	f := float64(n)
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d%s", n, units[i])
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

// HumanRate formats a bytes-per-second value (e.g. 1536 → "1.50KB/s").
func HumanRate(bps float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for bps >= 1024 && i < len(units)-1 {
		bps /= 1024
		i++
	}
	return fmt.Sprintf("%.2f%s/s", bps, units[i])
}

// FmtETA formats a seconds duration as H:MM:SS, or "--:--:--" when unknown.
func FmtETA(sec float64) string {
	if sec < 0 || math.IsInf(sec, 0) || math.IsNaN(sec) {
		return "--:--:--"
	}
	s := int(sec)
	return fmt.Sprintf("%d:%02d:%02d", s/3600, (s%3600)/60, s%60)
}
