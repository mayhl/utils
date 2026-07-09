// Package render is the single implementation of the mayhl_utils house visual
// spec on the Go side (palette + glyphs), mirroring lib/log.sh's mu_log. External
// pure-Python tools implement the same spec via Rich — parity is by shared
// contract, not a shared runtime.
package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
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

// PlainFlag is bound to the root command's --plain: force borderless tables.
var PlainFlag bool

// Plain reports whether rich rendering (borders, panels, color) should be skipped:
// piped/--plain (plainMode) or NO_COLOR/dumb (colorOff). Callers outside render use it
// to choose a plain-text path so pipes and NO_COLOR stay clean.
func Plain() bool { return plainMode() || colorOff() }

// plainMode reports whether tables render borderless/tab-aligned instead of the
// house rounded box. Precedence: --plain > MU_RENDER=plain|pretty > auto (plain
// unless stdout is a TTY, so piped/redirected/CI output stays parseable). Mirrors
// the MU_RENDER contract in gsw (git-signwip.sh).
func plainMode() bool {
	if PlainFlag {
		return true
	}
	switch os.Getenv("MU_RENDER") {
	case "plain":
		return true
	case "pretty":
		return false
	}
	return !term.IsTerminal(os.Stdout.Fd())
}

// logLine prints one house-style status line to stderr: a colored glyph tag
// followed by the message. A multi-line message tails below the header — each
// continuation line indented under the message text and dimmed (the Detail idiom) —
// so a wrapped error or backtrace reads as one block instead of running ragged
// flush-left. Meaning never rides on color alone — the glyph (or ASCII label under
// MU_ASCII) carries it.
func logLine(utf, ascii string, colors text.Colors, msg string) {
	tag := glyph(utf, ascii)
	head, tail, multi := strings.Cut(msg, "\n")
	if !colorOff() {
		tag = colors.Sprint(tag)
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", tag, head)
	if !multi {
		return
	}
	for _, line := range strings.Split(tail, "\n") {
		line = "  " + line // align under the message, past the 1-col glyph + space
		if !colorOff() {
			line = text.Colors{text.FgHiBlack}.Sprint(line)
		}
		fmt.Fprintln(os.Stderr, line)
	}
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
