package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
)

const barWidth = 30

// ProgressBar draws a single in-place rsync transfer bar — label, bar, percent,
// rate, ETA — redrawn on one line via carriage return. It mirrors the retired
// Rich aggregate bar (cyan label, right-aligned rate, "ETA" prefix). Values are
// fed from rsync's --info=progress2 output, not derived here.
//
// Like the log lines, the bar is human-facing UI and goes to stderr (as git,
// curl, and docker do) so stdout stays clean/pipeable. It renders only to a TTY;
// when stderr is a pipe/file, updates are no-ops (the transfer still runs).
type ProgressBar struct {
	label string
	tty   bool
	drawn bool
}

// NewProgressBar creates a bar with the given left-hand label.
func NewProgressBar(label string) *ProgressBar {
	info, err := os.Stderr.Stat()
	tty := err == nil && info.Mode()&os.ModeCharDevice != 0
	return &ProgressBar{label: label, tty: tty}
}

// Update redraws the bar at pct (clamped 0–100) with the given rate and ETA
// strings (rsync's own formatting, e.g. "12.34MB/s", "0:00:12").
func (p *ProgressBar) Update(pct int, rate, eta string) {
	if !p.tty {
		return
	}
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	filled := pct * barWidth / 100
	fillCh, emptyCh := "█", "░"
	if asciiMode() {
		fillCh, emptyCh = "#", "-"
	}
	bar := strings.Repeat(fillCh, filled) + strings.Repeat(emptyCh, barWidth-filled)
	// Keep the whole line within the terminal: cap the label to whatever's left
	// after the bar and the fixed pct/rate/eta columns (~33 cols) so a long
	// current-filename label truncates instead of wrapping.
	label := p.label
	tw := termWidth()
	if tw <= 0 {
		tw = 80
	}
	if budget := tw - barWidth - 33; budget >= 4 {
		label = truncLeft(label, budget)
	}
	if !colorOff() {
		label = text.Colors{text.FgCyan}.Sprint(label)
		bar = text.Colors{text.FgGreen}.Sprint(bar)
	}
	// \r returns to line start; \033[K clears any longer previous line.
	fmt.Fprintf(os.Stderr, "\r%s  %s %3d%%  %11s  ETA %s\033[K", label, bar, pct, rate, eta)
	p.drawn = true
}

// SetLabel updates the bar's left-hand label (e.g. the current file being
// transferred). The raw value is stored; Update truncates it to fit the terminal.
func (p *ProgressBar) SetLabel(s string) { p.label = s }

// Finish ends the bar's line. A no-op if nothing was ever drawn (non-TTY, or a
// transfer too small to emit a progress line), so no stray blank line appears.
func (p *ProgressBar) Finish() {
	if p.tty && p.drawn {
		fmt.Fprintln(os.Stderr)
	}
}
