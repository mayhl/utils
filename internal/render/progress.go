package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
)

// Bar geometry: barWidth is the roomy default, minBarWidth the floor a bar stays
// readable at, and barFixedCols the non-bar, non-label furniture on the line — the two
// gaps, " 100%", the 11-col rate field, and " ETA 0:00:12".
const (
	barWidth      = 30
	minBarWidth   = 10
	minLabelWidth = 8
	barFixedCols  = 33
)

// planBar sizes the bar and the label to the terminal: the bar keeps its full width while
// the label still has room, then SHRINKS (to minBarWidth) rather than letting the line
// wrap — a wrapped in-place bar smears across the scrollback, since \r only returns to the
// start of the last line. Under ~43 columns even the floor bar plus the fixed furniture
// won't fit; the label budget goes to 0 (Update drops the label) and the line is as short
// as it can be. An unknown width (piped) assumes 80.
func planBar(tw int) (bar, label int) {
	if tw <= 0 {
		tw = 80
	}
	tw-- // leave the last column empty: writing into it arms an auto-wrap on some terminals
	bar = tw - barFixedCols - minLabelWidth
	switch {
	case bar > barWidth:
		bar = barWidth
	case bar < minBarWidth:
		bar = minBarWidth
	}
	if label = tw - barFixedCols - bar; label < 0 {
		label = 0
	}
	return bar, label
}

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
	rate  string
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
	p.rate = rate
	// Keep the whole line within the terminal: the bar sizes to the width first, then a
	// long current-filename label truncates into whatever's left, so neither wraps.
	bw, budget := planBar(termWidth())
	filled := pct * bw / 100
	fillCh, emptyCh := "█", "░"
	if asciiMode() {
		fillCh, emptyCh = "#", "-"
	}
	bar := strings.Repeat(fillCh, filled) + strings.Repeat(emptyCh, bw-filled)
	label := ""
	if budget >= 4 { // below that there's no room for a meaningful label — drop it
		label = truncLeft(p.label, budget)
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

// Complete snaps the bar to 100% before ending it. rsync's --info=progress2
// percentage tracks bytes against the whole tree's total, so it settles below
// 100% when trailing files are skipped as up-to-date; on a clean exit the
// transfer is done, so redraw full. Only redraws if a bar was already shown, so
// a no-transfer run stays quiet.
func (p *ProgressBar) Complete() {
	if p.tty && p.drawn {
		p.Update(100, p.rate, "0:00:00")
	}
	p.Finish()
}

// Finish ends the bar's line. A no-op if nothing was ever drawn (non-TTY, or a
// transfer too small to emit a progress line), so no stray blank line appears.
func (p *ProgressBar) Finish() {
	if p.tty && p.drawn {
		fmt.Fprintln(os.Stderr)
	}
}
