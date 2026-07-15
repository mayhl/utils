// Package render is the single implementation of the mayhl_utils house visual
// spec on the Go side (palette + glyphs), mirroring lib/log.sh's mu_log. External
// pure-Python tools implement the same spec via Rich — parity is by shared
// contract, not a shared runtime.
package render

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/x/term"
	"github.com/jedib0t/go-pretty/v6/text"
)

// The keys that commit a widget's edits — Form submits, Editor saves.
//
// ctrl+s is the convention, but it is NOT reliably deliverable: on a tty with flow control
// left on it is XOFF (the terminal eats it to pause output), and a user who remaps the
// modifiers Mac-to-PC can end up sending a chord the terminal never forwards as ctrl. So a
// commit key must ALSO exist that needs no modifier at all — and it can't be a letter,
// because a letter types itself into whatever field has the cursor. F2 is what's left.
const (
	saveKey    = "ctrl+s"
	altSaveKey = "f2"
)

// saveHint is the footer's name for those keys, so every widget hints them identically.
const saveHint = "F2/ctrl+s save"

// glyph returns the UTF-8 form, or the ASCII fallback when asciiMode() is on. One gate
// for every house glyph — status tags, table cells, progress bars, the picker — so a
// PuTTY/latin1 session degrades uniformly instead of mojibaking static output while only
// the picker falls back. Colors mirror the shell/Python framework: INFO cyan, OK green,
// WARN yellow, ERROR red.
func glyph(utf, ascii string) string {
	if asciiMode() {
		return ascii
	}
	return utf
}

// asciiMode reports whether output should fall back to ASCII glyphs/box: MU_ASCII set, or
// a non-UTF-8 locale (PuTTY often defaults to C/latin1, which mojibakes box-drawing +
// glyphs). An unset locale is treated as UTF-8-capable. The single ASCII gate for the whole
// render package — status lines, tables, progress bars, and the picker all route through it.
func asciiMode() bool {
	if os.Getenv("MU_ASCII") != "" {
		return true
	}
	for _, v := range []string{os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE"), os.Getenv("LANG")} {
		if v != "" {
			u := strings.ToUpper(v)
			return !strings.Contains(u, "UTF-8") && !strings.Contains(u, "UTF8")
		}
	}
	return false
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

// crlf makes the house lines end \r\n instead of \n.
//
// A pty session (`ssh -t`) puts the LOCAL terminal in raw mode, where ONLCR is off: a bare
// \n moves the cursor down WITHOUT returning it to column 0, so every line mu prints while
// that session is up starts further right than the last — the staircase. A caller holding a
// pty open turns this on for the duration; nothing else needs to know.
var crlf atomic.Bool

// SetCRLF is set by a command that hands the terminal to a remote pty (see `mu job shell`).
func SetCRLF(on bool) { crlf.Store(on) }

// nl is the line ending the house currently prints.
func nl() string {
	if crlf.Load() {
		return "\r\n"
	}
	return "\n"
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
	fmt.Fprintf(os.Stderr, "%s %s%s", tag, head, nl())
	if !multi {
		return
	}
	for _, line := range strings.Split(tail, "\n") {
		line = "  " + line // align under the message, past the 1-col glyph + space
		if !colorOff() {
			line = text.Colors{text.FgHiBlack}.Sprint(line)
		}
		fmt.Fprint(os.Stderr, line+nl())
	}
}

// Info, OK, Warn, Err (the log tiers) live in log.go — they route through the
// slog houseHandler so every line also lands in framework.log. logLine below is
// the shared rendering primitive they and renderTier use.

// Detail prints a dim, glyph-less supporting line to stderr (e.g. the local/remote
// block under a verbose sshfs mount).
func Detail(msg string) {
	if IsQuiet() {
		return // -q trims the gray entirely — results, warnings, and errors only
	}
	if colorOff() {
		fmt.Fprint(os.Stderr, msg+nl())
		return
	}
	fmt.Fprint(os.Stderr, text.Colors{text.FgHiBlack}.Sprint(msg)+nl())
}

// Glyph is the exported ASCII gate, for callers outside render that draw their own lines
// (e.g. `mu config`'s resolved view). Same rule as the internal one: MU_ASCII or a non-UTF-8
// locale falls back.
func Glyph(utf, ascii string) string { return glyph(utf, ascii) }
