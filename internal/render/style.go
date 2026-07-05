// Package render is the single implementation of the mayhl_utils house visual
// spec on the Go side (palette + glyphs), mirroring lib/log.sh's mu_log. External
// pure-Python tools implement the same spec via Rich — parity is by shared
// contract, not a shared runtime.
package render

import (
	"fmt"
	"os"

	"github.com/jedib0t/go-pretty/v6/text"
)

// Level glyphs (UTF-8, with ASCII fallback under MU_ASCII), matching mu_log.
// Colors mirror the shell/Python framework: INFO cyan, OK green, WARN yellow,
// ERROR red.
func glyph(utf, ascii string) string {
	if os.Getenv("MU_ASCII") != "" {
		return ascii
	}
	return utf
}

// colorOff reports whether ANSI styling should be suppressed (NO_COLOR, or a
// dumb terminal). go-pretty honors this globally when we disable its colors.
func colorOff() bool {
	return os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
}

// logLine prints one house-style status line to stderr: a colored glyph tag
// followed by the message. Meaning never rides on color alone — the glyph (or
// ASCII label under MU_ASCII) carries it.
func logLine(utf, ascii string, colors text.Colors, msg string) {
	tag := glyph(utf, ascii)
	if !colorOff() {
		tag = colors.Sprint(tag)
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", tag, msg)
}

// Info, OK, Warn, Err (the log tiers) live in log.go — they route through the
// slog houseHandler so every line also lands in framework.log. logLine below is
// the shared rendering primitive they and renderTier use.

// Detail prints a dim, glyph-less supporting line to stderr (e.g. the local/remote
// block under a verbose sshfs mount).
func Detail(msg string) {
	if colorOff() {
		fmt.Fprintln(os.Stderr, msg)
		return
	}
	fmt.Fprintln(os.Stderr, text.Colors{text.FgHiBlack}.Sprint(msg))
}
