package render

import "charm.land/lipgloss/v2"

// House UI primitives for bordered layouts (help panels, summaries). Domain-free —
// no cobra/CLI knowledge — so they feed [[houseui-extraction-idea]] unchanged. Callers
// gate on Plain() before using these (they always emit color/borders).

// TermWidth is the terminal column count ($COLUMNS or the stdout tty), or 0 if unknown.
func TermWidth() int { return termWidth() }

// Panel renders a rounded box in the house palette (dim border). The title is taken
// AS GIVEN (caller styles it — see Bold), so panels can carry different title hues.
// width > 0 fixes the content width — body text soft-wraps to it, and a shared width
// aligns a stack of panels; width <= 0 fits the content.
func Panel(title, body string, width int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(HueDim)).
		Padding(0, 1)
	if width > 0 {
		box = box.Width(width)
	}
	content := title
	if body != "" {
		content += "\n" + body
	}
	return box.Render(content)
}

// Bold renders text in a palette hue, bold — for titles and section headers.
func Bold(text, hue string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hue)).Bold(true).Render(text)
}

// Badge renders a bracketed colored label, e.g. "[opt-in]", in the given palette hue
// (falls back to HueDim). Cool hues only per the color policy — warm = status.
func Badge(text, hue string) string {
	if hue == "" {
		hue = HueDim
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hue)).Render("[" + text + "]")
}

// Fg colors text with a palette hue (lipgloss). The house "paint this string" helper.
func Fg(text, hue string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hue)).Render(text)
}
