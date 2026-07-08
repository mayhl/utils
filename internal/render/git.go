package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/mayhl/mayhl_utils/internal/git"
)

const unreviewedPrefix = "[unreviewed] "

// subjectFit is the SUBJECT column enforcer: it swaps the verbose "[unreviewed] "
// prefix for a red ⚑ marker AND truncates to maxLen display cols with a trailing … so a
// long subject never soft-wraps the panel. Both live here (not a Transformer) so
// truncation stays ANSI-safe — truncRight runs on the plain text, before the marker's
// color is added. [unreviewed] = agent WIP, a deliberate rare use of red per the policy.
func subjectFit(s string, maxLen int) string {
	flagged := strings.HasPrefix(s, unreviewedPrefix)
	if flagged {
		s = s[len(unreviewedPrefix):]
	}
	avail := maxLen
	if flagged {
		avail -= 2 // "⚑ " marker
	}
	if avail < 1 {
		avail = 1
	}
	s = truncRight(s, avail)
	if flagged {
		return text.Colors{text.FgRed}.Sprint(glyph("⚑", "!")) + " " + s
	}
	return s
}

// subjectBudget is the SUBJECT column's max display width: terminal width minus the
// fixed columns and rounded-table overhead (3 per column + 1). Unknown width (piped) →
// no cap (full subject, no data loss on a pipe).
func subjectBudget(fixedCols, ncols int) int {
	tw := termWidth()
	if tw <= 0 {
		return 1 << 20
	}
	if b := tw - fixedCols - (3*ncols + 1); b > 12 {
		return b
	}
	return 12
}

// gitHashColor tints commit hashes FgYellow, matching git's own --oneline (an
// accepted exception to warm-hues-are-status for git-native output). ANSI yellow is
// theme-owned — bright on dark, dark/readable on light. MU_WHITE is the escape hatch
// for a terminal whose yellow reads badly: set it to any value to drop the tint to
// plain white instead.
func gitHashColor() text.Colors {
	if os.Getenv("MU_WHITE") != "" {
		return text.Colors{text.FgWhite}
	}
	return text.Colors{text.FgYellow}
}

// gitTable builds a house-styled rounded table (cyan title/headers, dim frame),
// shared by both preview renderers.
func gitTable(title string) table.Writer {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	applyStyle(t)
	t.Style().Title.Colors = text.Colors{text.FgCyan, text.Bold}
	t.Style().Color.Header = text.Colors{text.FgCyan}
	t.Style().Color.Border = text.Colors{text.FgHiBlack}
	t.Style().Color.Separator = text.Colors{text.FgHiBlack}
	t.SetTitle(title)
	return t
}

// GitSignwip renders the read-only signwip preview: which unsigned WIP would sign
// vs skip ([unreviewed]). Meaning rides the ACT label; color is accent only.
func GitSignwip(s git.Signwip) {
	t := gitTable(fmt.Sprintf("git signwip — %d to sign, %d %s skipped", s.ToSign, s.Tagged, glyph("⚑", "!")))
	t.AppendHeader(table.Row{"ACT", "HASH", "SUBJECT"})
	actW, hashW := len("ACT"), len("HASH")
	for _, r := range s.Rows {
		t.AppendRow(table.Row{r.Act, r.Hash, r.Subject})
		actW, hashW = max(actW, len(r.Act)), max(hashW, len(r.Hash))
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Name: "ACT", Transformer: signwipActTransformer},
		{Name: "HASH", Colors: gitHashColor()},
		{Name: "SUBJECT", WidthMax: subjectBudget(actW+hashW, 3), WidthMaxEnforcer: subjectFit},
	})
	t.Render()
	gitLegend(s.Tagged > 0)
}

// GitReviewed renders the read-only `git reviewed` preview: which [unreviewed] WIP would
// be un-tagged (oldest-first) vs kept. The ACT label carries the action; the ⚑ still
// flags every [unreviewed] subject.
func GitReviewed(r git.Reviewed) {
	var title string
	if r.Untag == r.Tagged {
		title = fmt.Sprintf("git reviewed — un-tag all %d %s", r.Tagged, glyph("⚑", "!"))
	} else {
		title = fmt.Sprintf("git reviewed — un-tag %d of %d %s", r.Untag, r.Tagged, glyph("⚑", "!"))
	}
	t := gitTable(title)
	t.AppendHeader(table.Row{"ACT", "HASH", "SUBJECT"})
	actW, hashW := len("ACT"), len("HASH")
	for _, row := range r.Rows {
		t.AppendRow(table.Row{row.Act, row.Hash, row.Subject})
		actW, hashW = max(actW, len(row.Act)), max(hashW, len(row.Hash))
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Name: "ACT", Transformer: reviewedActTransformer},
		{Name: "HASH", Colors: gitHashColor()},
		{Name: "SUBJECT", WidthMax: subjectBudget(actW+hashW, 3), WidthMaxEnforcer: subjectFit},
	})
	t.Render()
	gitLegend(r.Tagged > 0)
}

// reviewedActTransformer colors the ACT cell: green untag (being cleared), magenta keep
// (stays tagged), and dim for clean / base (not acted on).
func reviewedActTransformer(v any) string {
	s := fmt.Sprint(v)
	switch s {
	case "untag":
		return text.Colors{text.FgGreen}.Sprint(s)
	case "keep":
		return text.Colors{text.FgMagenta}.Sprint(s)
	default: // "clean" | "base"
		return text.Colors{text.FgHiBlack}.Sprint(s)
	}
}

// GitPushsigned renders the read-only pushsigned preview: the contiguous signed
// prefix that would push vs the WIP held local. The ✓/✗ glyph carries signedness.
func GitPushsigned(p git.Pushsigned) {
	t := gitTable(fmt.Sprintf("git pushsigned — %d push, %d held ahead of %s", p.PushN, p.Held, p.Upstream))
	t.AppendHeader(table.Row{"", "TAG", "HASH", "SUBJECT"})
	tagW, hashW := len("TAG"), len("HASH")
	for _, r := range p.Rows {
		t.AppendRow(table.Row{pushGlyph(r), pushTag(r), r.Hash, r.Subject})
		tagW = max(tagW, len(pushTag(r)))
		hashW = max(hashW, len(r.Hash))
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Name: "TAG", Transformer: pushTagTransformer},
		{Name: "HASH", Colors: gitHashColor()},
		{Name: "SUBJECT", WidthMax: subjectBudget(1+tagW+hashW, 4), WidthMaxEnforcer: subjectFit},
	})
	t.Render()
	gitLegend(pushHasFlag(p))
}

// gitLegend prints the ⚑ key below a preview when any [unreviewed] row is shown, so
// the marker is self-explanatory. Colors no-op in plain/NO_COLOR (glyph still carries).
func gitLegend(show bool) {
	if !show {
		return
	}
	flag := text.Colors{text.FgRed}.Sprint(glyph("⚑", "!"))
	key := text.Colors{text.FgHiBlack}.Sprint("[unreviewed] — agent WIP, not yet reviewed")
	fmt.Fprintln(os.Stdout, "  "+flag+" "+key)
}

func pushHasFlag(p git.Pushsigned) bool {
	for _, r := range p.Rows {
		if strings.HasPrefix(r.Subject, unreviewedPrefix) {
			return true
		}
	}
	return false
}

// signwipActTransformer colors the ACT cell: green sign, magenta skip, and the base
// (newest signed) row dim — it anchors the stack but isn't itself acted on.
func signwipActTransformer(v any) string {
	s := fmt.Sprint(v)
	switch s {
	case "sign":
		return text.Colors{text.FgGreen}.Sprint(s)
	case "skip":
		return text.Colors{text.FgMagenta}.Sprint(s)
	default: // "base"
		return text.Colors{text.FgHiBlack}.Sprint(s)
	}
}

func pushTagTransformer(v any) string {
	s := fmt.Sprint(v)
	if s == "push" {
		return text.Colors{text.FgGreen}.Sprint(s)
	}
	return text.Colors{text.FgHiBlack}.Sprint(s)
}

// pushGlyph is a green ✓ for a commit that would push or is itself signed, else a
// red ✗ (an unsigned commit held behind the signed prefix).
func pushGlyph(r git.PushRow) string {
	if r.Push || r.Signed {
		return text.Colors{text.FgGreen, text.Bold}.Sprint(glyph("✓", "OK"))
	}
	return text.Colors{text.FgRed, text.Bold}.Sprint(glyph("✗", "X"))
}

func pushTag(r git.PushRow) string {
	if r.Push {
		return "push"
	}
	return "hold"
}
