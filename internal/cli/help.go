package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mayhl/mayhl_utils/internal/render"
)

// House help renderer: a reusable, opt-in wrapper that renders any cobra command as
// house-styled bordered panels (synopsis / commands / flags / examples). Opt a module
// in with wrapHelp(cmd) — the whole subtree inherits it (Cobra calls the nearest
// SetHelpFunc). Zero-config: reads standard cobra fields, so any command works. Falls
// back to plain text under render.Plain() (piped/--plain/NO_COLOR) so pipes stay clean.

const (
	annHelpLabel         = "render.helpLabel"         // colored badge text on a command
	annHelpHue           = "render.helpHue"           // palette hue for the badge
	annHelpTitle         = "render.helpTitle"         // human-readable heading for the synopsis
	annHelpShortcuts     = "render.helpShortcuts"     // shell front-doors ("name\twhat it runs" per line)
	annHelpShortcutsNote = "render.helpShortcutsNote" // one-line lead above the Shortcuts list
)

// Help panel color roles. Help is a DELIBERATE exception to the app-wide color policy
// (warm hues reserved for status) — it shows no status, so the usage line doubles as a
// color legend and each panel's items match their token. Retune all in one place.
const (
	hueTitle   = render.HueWarn // color1: <title>                        (yellow, bold)
	hueCommand = render.HueID   // color2: [command] + Commands panel     (cyan)
	hueFlag    = render.HueUser // color3: [--flags] + Flags panel        (magenta)
	hueRoot    = render.HueOK   // color4: <root path> + Shortcut panel   (green)
	hueBadge   = render.HueDim  // color5: badges                         (gray)
)

// wrapHelp opts a command and its subtree into the house help renderer.
func wrapHelp(c *cobra.Command) { c.SetHelpFunc(houseHelp) }

// MaybeRootHelp renders house help for the ROOT command when args request bare-root help
// (`mu`, `mu -h`, `mu --help`, `mu help`), returning true if it handled the request. fang
// hardcodes root.SetHelpFunc with no override, so main() calls this before fang.Execute to
// keep the root's help in the house language; subcommand help still flows through fang→
// cobra to each command's wrapHelp. The obscure `mu <flag> --help` (a flag before --help)
// falls through to fang.
func MaybeRootHelp(root *cobra.Command, args []string) bool {
	if !wantsRootHelp(args) {
		return false
	}
	root.InitDefaultHelpFlag() // so the Flags panel lists -h/--help
	houseHelp(root, nil)
	return true
}

func wantsRootHelp(args []string) bool {
	switch len(args) {
	case 0:
		return true // bare `mu`
	case 1:
		return args[0] == "-h" || args[0] == "--help" || args[0] == "help"
	default:
		return false
	}
}

// setHelpTitle sets a human-readable heading for a command's synopsis panel (e.g.
// "Git Workflow Previews"); without it the command path is used.
func setHelpTitle(c *cobra.Command, title string) {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[annHelpTitle] = title
}

func helpTitle(c *cobra.Command) string {
	if c.Annotations != nil {
		if t := c.Annotations[annHelpTitle]; t != "" {
			return t
		}
	}
	return c.CommandPath()
}

// setHelpShortcuts declares a command's shell front-doors, shown as a Shortcuts panel
// (color4, matching the <root> usage token). Each pair is {shortcut, what it runs}.
func setHelpShortcuts(c *cobra.Command, pairs ...[2]string) {
	rows := make([]string, len(pairs))
	for i, p := range pairs {
		rows[i] = p[0] + "\t" + p[1]
	}
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[annHelpShortcuts] = strings.Join(rows, "\n")
}

// setHelpShortcutsNote sets a short lead line rendered (dimmed) above the Shortcuts list —
// e.g. explaining that shell front-doors exist and where a command's own set lives.
func setHelpShortcutsNote(c *cobra.Command, note string) {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[annHelpShortcutsNote] = note
}

func helpShortcutsNote(c *cobra.Command) string {
	if c.Annotations == nil {
		return ""
	}
	return c.Annotations[annHelpShortcutsNote]
}

func helpShortcuts(c *cobra.Command) [][2]string {
	if c.Annotations == nil || c.Annotations[annHelpShortcuts] == "" {
		return nil
	}
	var out [][2]string
	for _, line := range strings.Split(c.Annotations[annHelpShortcuts], "\n") {
		name, desc, _ := strings.Cut(line, "\t")
		out = append(out, [2]string{name, desc})
	}
	return out
}

// setHelpLabel attaches a colored badge to a command, shown by its name both in its own
// help header and in its parent's Commands panel. hue is a render.Hue* const.
func setHelpLabel(c *cobra.Command, text, hue string) {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[annHelpLabel] = text
	c.Annotations[annHelpHue] = hue
}

func helpLabel(c *cobra.Command) (text, hue string) {
	if c.Annotations == nil {
		return "", ""
	}
	return c.Annotations[annHelpLabel], c.Annotations[annHelpHue]
}

// visibleSubs is the command's user-facing subcommands (drops hidden + the auto `help`).
func visibleSubs(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, s := range c.Commands() {
		if s.IsAvailableCommand() && s.Name() != "help" {
			out = append(out, s)
		}
	}
	return out
}

// helpMaxWidth caps panel width on very wide terminals so lines stay readable.
const helpMaxWidth = 100

func houseHelp(c *cobra.Command, _ []string) {
	if render.Plain() {
		plainHelp(c)
		return
	}
	// One shared width for every panel: fill the terminal (capped), so descriptions
	// soft-wrap and the panels line up. inner = outer − rounded border(2) − padding(2).
	outer := render.TermWidth()
	if outer <= 0 {
		outer = 80
	}
	if outer > helpMaxWidth {
		outer = helpMaxWidth
	}
	boxW := max(outer-2, 24) // width passed to lipgloss (border + padding included)
	textW := boxW - 4        // usable text: minus rounded border (2) + L/R padding (2)

	var b strings.Builder

	// Synopsis: title (color1) + badge (color5), the colored usage legend, then the desc.
	title := render.Bold(helpTitle(c), hueTitle)
	if t, _ := helpLabel(c); t != "" {
		title += " " + render.Badge(t, hueBadge)
	}
	syn := usageLegend(c)
	desc := c.Long
	if desc == "" {
		desc = c.Short
	}
	if desc != "" {
		syn += "\n\n" + flow(desc)
	}
	b.WriteString(render.Panel(title, syn, boxW) + "\n")

	if subs := visibleSubs(c); len(subs) > 0 {
		w := 0
		for _, s := range subs {
			if len(s.Name()) > w {
				w = len(s.Name())
			}
		}
		lines := make([]string, 0, len(subs))
		for _, s := range subs {
			badge, bw := "", 0
			if t, _ := helpLabel(s); t != "" {
				badge = render.Badge(t, hueBadge)
				bw = len(t) + 2 // "[text]"
			}
			lines = append(lines, wrapItem(s.Name(), hueCommand, s.Short, badge, bw, w, textW))
		}
		b.WriteString(render.Panel(render.Bold("Commands", hueCommand), strings.Join(lines, "\n"), boxW) + "\n")
	}

	if fl := flagLines(c.LocalFlags(), hueFlag, textW); len(fl) > 0 {
		b.WriteString(render.Panel(render.Bold("Flags", hueFlag), strings.Join(fl, "\n"), boxW) + "\n")
	}
	if sc := helpShortcuts(c); len(sc) > 0 {
		w := 0
		for _, p := range sc {
			if len(p[0]) > w {
				w = len(p[0])
			}
		}
		lines := make([]string, 0, len(sc))
		for _, p := range sc {
			lines = append(lines, wrapItem(p[0], hueRoot, p[1], "", 0, w, textW))
		}
		body := strings.Join(lines, "\n")
		if note := helpShortcutsNote(c); note != "" {
			body = render.Fg(flow(note), hueBadge) + "\n\n" + body
		}
		b.WriteString(render.Panel(render.Bold("Shortcuts", hueRoot), body, boxW) + "\n")
	}
	if c.Example != "" {
		b.WriteString(render.Panel(render.Bold("Examples", render.HueName), strings.TrimRight(c.Example, "\n"), boxW) + "\n")
	}
	fmt.Fprint(os.Stdout, b.String())
}

// usageLegend renders "<root path> [command] [--flags]" as a colored key — path in
// color4 (shortcuts), [command] in color2 (Commands items), [--flags] in color3 (Flags
// items) — so the synopsis teaches the panel color code.
func usageLegend(c *cobra.Command) string {
	s := render.Fg(c.CommandPath(), hueRoot)
	if c.HasAvailableSubCommands() {
		s += " " + render.Fg("[command]", hueCommand)
	}
	if c.HasAvailableFlags() {
		s += " " + render.Fg("[--flags]", hueFlag)
	}
	return s
}

// flagLines formats a flag set as aligned "-x, --name  usage" lines with the name in the
// given hue. Custom-rendered because pflag's FlagUsages emits one preformatted block we
// can't tint per-flag.
func flagLines(fs *pflag.FlagSet, hue string, width int) []string {
	type fl struct{ name, usage string }
	var items []fl
	w := 0
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", " + name
		}
		if len(name) > w {
			w = len(name)
		}
		items = append(items, fl{name, f.Usage})
	})
	lines := make([]string, 0, len(items))
	for _, it := range items {
		lines = append(lines, wrapItem(it.name, hue, it.usage, "", 0, w, width))
	}
	return lines
}

// wrapItem lays out one panel row: the name (nameHue, padded to nameW) + "  " + the
// description word-wrapped to the panel width, with continuation lines hang-indented
// under the description. An optional pre-styled badge (display width badgeW) trails the
// last line, dropping to its own indented line if it doesn't fit.
func wrapItem(name, nameHue, desc, badge string, badgeW, nameW, width int) string {
	const gap = 2
	indentW := nameW + gap
	avail := width - indentW
	if avail < 8 {
		avail = 8
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(desc) {
		switch {
		case cur == "":
			cur = word
		case len(cur)+1+len(word) <= avail:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" || len(lines) == 0 {
		lines = append(lines, cur)
	}
	if badge != "" {
		last := len(lines) - 1
		switch {
		case lines[last] == "":
			lines[last] = badge
		case len(lines[last])+1+badgeW <= avail:
			lines[last] += " " + badge
		default:
			lines = append(lines, badge)
		}
	}
	out := render.Fg(fmt.Sprintf("%-*s", nameW, name), nameHue) + strings.Repeat(" ", gap) + lines[0]
	indent := strings.Repeat(" ", indentW)
	for _, l := range lines[1:] {
		out += "\n" + indent + l
	}
	return out
}

// flow collapses a description's source-readability line breaks into spaces so the panel
// can soft-wrap it to the shared width; blank-line paragraph breaks are preserved.
func flow(s string) string {
	paras := strings.Split(s, "\n\n")
	for i, p := range paras {
		paras[i] = strings.Join(strings.Fields(p), " ")
	}
	return strings.Join(paras, "\n\n")
}

// plainHelp is the borderless/no-color fallback (piped, --plain, NO_COLOR).
func plainHelp(c *cobra.Command) {
	w := os.Stdout
	fmt.Fprintln(w, helpTitle(c))
	fmt.Fprintln(w, c.UseLine())
	desc := c.Long
	if desc == "" {
		desc = c.Short
	}
	if desc != "" {
		fmt.Fprintf(w, "\n%s\n", desc)
	}
	if subs := visibleSubs(c); len(subs) > 0 {
		fmt.Fprintln(w, "\nCommands:")
		for _, s := range subs {
			line := fmt.Sprintf("  %-12s %s", s.Name(), s.Short)
			if t, _ := helpLabel(s); t != "" {
				line += " [" + t + "]"
			}
			fmt.Fprintln(w, line)
		}
	}
	if f := c.LocalFlags().FlagUsages(); strings.TrimSpace(f) != "" {
		fmt.Fprintf(w, "\nFlags:\n%s", f)
	}
	if sc := helpShortcuts(c); len(sc) > 0 {
		fmt.Fprintln(w, "\nShortcuts:")
		if note := helpShortcutsNote(c); note != "" {
			fmt.Fprintf(w, "  %s\n", flow(note))
		}
		for _, p := range sc {
			fmt.Fprintf(w, "  %-12s %s\n", p[0], p[1])
		}
	}
	if c.Example != "" {
		fmt.Fprintf(w, "\nExamples:\n%s\n", c.Example)
	}
}
