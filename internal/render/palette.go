package render

import (
	"charm.land/lipgloss/v2"
	"github.com/jedib0t/go-pretty/v6/text"
)

// House semantic palette: one source of truth for "which concept → which hue," as
// ANSI indices so both render backends express the same color. Warm hues are
// RESERVED for status (state glyphs, walltime burn); columns draw from the cool set
// + dim, so status always reads unambiguously. Tables and the picker name CONCEPTS
// (id, user, location…), not raw colors, and convert via tc() (go-pretty) / lg()
// (lipgloss). Deliberately just a named palette + two thin adapters — NOT a generic
// column-coloring engine (deferred; see the render-color-standardization note).
const (
	HueID    = "6"  // cyan       — ids (PID, job id), titles, headers
	HueUser  = "5"  // magenta    — user / owner
	HueLoc   = "4"  // blue       — host, node, cluster, path
	HueGroup = "12" // bright-blue — queue / partition / group / tag
	HueName  = "7"  // white      — name / free text
	HueOK    = "2"  // green      — selected / ok      (status-reserved warm)
	HueWarn  = "3"  // yellow     — filter / warn      (status-reserved warm)
	HueErr   = "1"  // red        — error / fail       (status-reserved warm)
	HueDim   = "8"  // hi-black   — frames, help, chrome
)

// ansiFg maps a house hue to its go-pretty foreground attribute.
var ansiFg = map[string]text.Color{
	HueID:    text.FgCyan,
	HueUser:  text.FgMagenta,
	HueLoc:   text.FgBlue,
	HueGroup: text.FgHiBlue,
	HueName:  text.FgWhite,
	HueOK:    text.FgGreen,
	HueWarn:  text.FgYellow,
	HueErr:   text.FgRed,
	HueDim:   text.FgHiBlack,
}

// tc is the go-pretty column color for a house hue (static tables).
func tc(hue string) text.Colors { return text.Colors{ansiFg[hue]} }

// lg is the lipgloss style for a house hue (interactive views).
func lg(hue string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hue))
}
