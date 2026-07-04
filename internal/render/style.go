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

// Info, OK, Warn, and Err mirror mu_log's tiers (glyphs → ✓ ! ✗; INFO cyan,
// OK green, WARN yellow, ERROR red).
func Info(msg string) { logLine("→", "[INFO]", text.Colors{text.FgCyan}, msg) }

// OK prints a house-style OK line to stderr.
func OK(msg string) { logLine("✓", "[OK]", text.Colors{text.FgGreen, text.Bold}, msg) }

// Warn prints a house-style WARN line to stderr.
func Warn(msg string) { logLine("!", "[WARN]", text.Colors{text.FgYellow, text.Bold}, msg) }

// Err prints a house-style ERROR line to stderr.
func Err(msg string) { logLine("✗", "[ERROR]", text.Colors{text.FgRed, text.Bold}, msg) }

// Detail prints a dim, glyph-less supporting line to stderr (e.g. the local/remote
// block under a verbose sshfs mount).
func Detail(msg string) {
	if colorOff() {
		fmt.Fprintln(os.Stderr, msg)
		return
	}
	fmt.Fprintln(os.Stderr, text.Colors{text.FgHiBlack}.Sprint(msg))
}
