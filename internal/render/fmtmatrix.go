package render

import (
	"fmt"
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// MatrixCell is one cell of a Matrix: a tool tagged by which source(s) provide it.
// Domain-free — the caller maps its data (e.g. doctor's fmt report) onto it.
type MatrixCell struct {
	Defined bool   // false → nothing tracked for this slot ("–")
	Tool    string // display name
	Mise    bool   // provided by the mise (enforced) stack
	Mason   bool   // provided by the Mason (editor) stack
	Drift   bool   // present in both, versions disagree
	Level   string // verdict: "ok" | "warn" | "error"
}

// MatrixRow is one row: a label, its worst verdict, and cells across the columns.
type MatrixRow struct {
	Label string
	Level string
	Cells []MatrixCell
}

// Matrix renders a titled grid whose first column is the row's verdict glyph and
// each cell a tool badged by source (◆ mise / ● mason / ◆● both / ○ missing, ⚠ on
// version drift), colored by verdict. A legend decodes the badges.
func Matrix(title string, cols []string, rows []MatrixRow) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	t.Style().Title.Colors = text.Colors{text.FgCyan, text.Bold}
	t.Style().Color.Header = text.Colors{text.FgCyan}
	t.Style().Color.Border = text.Colors{text.FgHiBlack}
	t.Style().Color.Separator = text.Colors{text.FgHiBlack}
	t.SetTitle(title)

	header := table.Row{"", cols[0]}
	for _, c := range cols[1:] {
		header = append(header, c)
	}
	t.AppendHeader(header)

	for _, r := range rows {
		row := table.Row{statusCell(r.Level), r.Label}
		for _, c := range r.Cells {
			row = append(row, matrixCell(c))
		}
		t.AppendRow(row)
	}
	// First data column (the row label) in bold magenta, like the other tables.
	t.SetColumnConfigs([]table.ColumnConfig{{Name: cols[0], Colors: text.Colors{text.FgMagenta, text.Bold}}})
	t.Render()
	fmt.Println(matrixLegend())
}

// matrixCell renders one cell: "tool <badge>", colored by verdict (or a dim dash
// for an undefined slot). The badge shape carries the source so meaning never
// rides on color alone.
func matrixCell(c MatrixCell) string {
	if !c.Defined {
		return dim(glyph("–", "-"))
	}
	s := c.Tool + " " + sourceBadge(c)
	if c.Drift {
		s += " " + glyph("⚠", "!")
	}
	if colorOff() {
		return s
	}
	return levelColors(c.Level).Sprint(s)
}

// sourceBadge encodes which stack(s) provide the tool.
func sourceBadge(c MatrixCell) string {
	// ASCII fallbacks echo the UTF shapes (+ filled, * dot, o empty) and stay
	// distinct from the legend's words and the "-" undefined-slot dash.
	switch {
	case c.Mise && c.Mason:
		return glyph("◆●", "+*")
	case c.Mise:
		return glyph("◆", "+")
	case c.Mason:
		return glyph("●", "*")
	default:
		return glyph("○", "o")
	}
}

func levelColors(level string) text.Colors {
	switch level {
	case "ok", "OK":
		return text.Colors{text.FgGreen}
	case "warn", "WARN":
		return text.Colors{text.FgYellow}
	case "error", "ERROR", "fail", "FAIL":
		return text.Colors{text.FgRed}
	default:
		return text.Colors{text.FgHiBlack}
	}
}

func matrixLegend() string {
	s := fmt.Sprintf("  %s mise (enforced)   %s mason (editor)   %s both   %s missing   %s version drift",
		glyph("◆", "+"), glyph("●", "*"), glyph("◆●", "+*"), glyph("○", "o"), glyph("⚠", "!"))
	return dim(s)
}

func dim(s string) string {
	if colorOff() {
		return s
	}
	return text.Colors{text.FgHiBlack}.Sprint(s)
}
