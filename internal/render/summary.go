package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
)

// Summary prints a house completion line to stderr: a green ✓, a headline, then
// dim "·"-separated detail fields — e.g.
//
//	✓ push → mike   3/42 files (39 unchanged) · 4.2GB · 47.3MB/s · 3.1× speedup
func Summary(headline string, parts []string) {
	tag := glyph("✓", "[OK]")
	detail := strings.Join(parts, " · ")
	if !colorOff() {
		tag = text.Colors{text.FgGreen, text.Bold}.Sprint(tag)
		detail = text.Colors{text.FgHiBlack}.Sprint(detail)
	}
	if detail != "" {
		detail = "   " + detail
	}
	fmt.Fprintf(os.Stderr, "%s %s%s\n", tag, headline, detail)
}
