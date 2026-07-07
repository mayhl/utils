package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/doctor"
	"github.com/mayhl/mayhl_utils/internal/render"
)

func doctorCmd() *cobra.Command {
	var verbose bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check the environment (tools, config, plugin checks).",
		Long: "Run built-in health checks (mise, config, ticket) plus any executable\n" +
			"plugins in the checks dir. Records the run to the event log (scope=doctor);\n" +
			"exits non-zero only if a check FAILs (WARN reports but doesn't block).",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			results, overall := doctor.Run()

			if verbose {
				// Verbose: split into separate tables — one per section, then a detail
				// table per check (TSV verbose → sub-table; prose → text block).
				var order []string
				groups := map[string][]render.StatusRow{}
				for _, r := range results {
					if _, seen := groups[r.Section]; !seen {
						order = append(order, r.Section)
					}
					groups[r.Section] = append(groups[r.Section],
						render.StatusRow{Level: levelStr(r.Status), Name: r.Name, Detail: r.Detail})
				}
				for _, sec := range order {
					render.StatusTable(titleCase(sec), groups[sec])
				}
				for _, r := range results {
					if r.Verbose == "" {
						continue
					}
					title := r.Name // sub-table uses the fuller Title when a check sets one
					if r.Title != "" {
						title = r.Title
					}
					fmt.Println()
					if rows, ok := verboseRows(r.Verbose); ok {
						render.StatusTable(title, rows)
					} else {
						fmt.Printf("%s:\n", title)
						for _, ln := range strings.Split(r.Verbose, "\n") {
							fmt.Printf("  %s\n", ln)
						}
					}
				}
			} else {
				// Default: one combined table of every check.
				rows := make([]render.StatusRow, len(results))
				for i, r := range results {
					rows[i] = render.StatusRow{Level: levelStr(r.Status), Name: r.Name, Detail: r.Detail}
				}
				render.StatusTable("Doctor", rows)
			}

			ok, warn, fail := tally(results)
			summary := fmt.Sprintf("%d ok, %d warn, %d fail", ok, warn, fail)
			switch {
			case fail > 0:
				render.EventErr("doctor", summary)
			case warn > 0:
				render.EventWarn("doctor", summary)
			default:
				render.EventOK("doctor", summary)
			}

			if overall == doctor.Fail {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().BoolVarP(&verbose, "verbose", "v", false, "show full per-check detail (plugin output, versions, expiry)")
	c.AddCommand(doctorFmtCmd())
	return c
}

// doctorFmtCmd is the `mu doctor fmt` module: the formatter/linter/debug/LSP
// matrix, each cell tagged by source and judged tier-aware (see doctor.FmtMatrix).
func doctorFmtCmd() *cobra.Command {
	var dumpConfig bool
	c := &cobra.Command{
		Use:   "fmt",
		Short: "Formatter/linter/debug/LSP matrix (mise enforced vs Mason editor).",
		Long: "Show the formatter/linter/debug/LSP stack as a language × role matrix, each\n" +
			"cell tagged by source: mise (the enforced fmt tier behind the git hook and\n" +
			"`mu fmt`) vs Mason (nvim's editor copy). Verdicts are tier-aware — the mise fmt\n" +
			"tier is opt-in, so a dormant mise isn't an error and Mason is the backup.\n\n" +
			"The declared-tool set is a built-in default embedded in mu; --dump-config prints\n" +
			"it. To customize without rebuilding, redirect that into ~/.config/mu/config.fmt.toml\n" +
			"and edit — when present it fully replaces the default.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if dumpConfig {
				_, err := os.Stdout.Write(doctor.EffectiveFmtConfig())
				return err
			}
			rep := doctor.FmtMatrix()

			cols := []string{"Language"}
			for _, role := range doctor.RoleOrder {
				cols = append(cols, role.String())
			}
			rows := make([]render.MatrixRow, len(rep.Rows))
			for i, r := range rep.Rows {
				cells := make([]render.MatrixCell, len(r.Cells))
				for j, c := range r.Cells {
					cells[j] = render.MatrixCell{
						Defined: c.Defined,
						Tool:    c.Tool,
						Mise:    c.Mise,
						Mason:   c.Mason,
						Drift:   c.Drift,
						Level:   levelStr(c.Status),
					}
				}
				rows[i] = render.MatrixRow{Label: r.Lang, Level: levelStr(r.Status), Cells: cells}
			}
			render.Matrix(fmtBanner(rep.TierOn), cols, rows)

			// Version drift + config tools the classifier didn't recognize, below the grid.
			// Dedup by tool — a tool spanning two roles (ruff: format+lint) drifts once.
			seen := map[string]bool{}
			for _, r := range rep.Rows {
				for _, c := range r.Cells {
					if c.Drift && !seen[c.Tool] {
						seen[c.Tool] = true
						fmt.Printf("  drift  %s: mise %s ≠ mason %s\n", c.Tool, c.MiseVer, c.MasonVer)
					}
				}
			}
			if len(rep.Unknown) > 0 {
				fmt.Printf("  unclassified in the fmt config: %s\n", strings.Join(rep.Unknown, ", "))
			}

			ok, warn, fail := tallyMatrix(rep)
			summary := fmt.Sprintf("fmt: %d ok, %d warn, %d fail", ok, warn, fail)
			switch {
			case fail > 0:
				render.EventErr("doctor", summary)
			case warn > 0:
				render.EventWarn("doctor", summary)
			default:
				render.EventOK("doctor", summary)
			}
			if rep.Status == doctor.Fail {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dumpConfig, "dump-config", false, "print the effective declared-tool TOML (embedded default, or the ~/.config/mu/config.fmt.toml override)")
	return c
}

// fmtBanner is the matrix title: name plus the current fmt-tier mode.
func fmtBanner(tierOn bool) string {
	if tierOn {
		return "Formatter / Linter Matrix\nfmt tier: ON — mise enforced (git hook + mu fmt)"
	}
	return "Formatter / Linter Matrix\nfmt tier: OFF — Mason active · MU_MODULES=fmt to enforce via mise"
}

// tallyMatrix counts defined cells by verdict for the event summary.
func tallyMatrix(rep doctor.FmtReport) (ok, warn, fail int) {
	for _, r := range rep.Rows {
		for _, c := range r.Cells {
			if !c.Defined {
				continue
			}
			switch c.Status {
			case doctor.OK:
				ok++
			case doctor.Warn:
				warn++
			default:
				fail++
			}
		}
	}
	return ok, warn, fail
}

// verboseRows parses tab-separated verbose ("level\tname\tdetail" per line) into
// StatusRows for a sub-table. Returns ok=false unless every non-empty line is TSV, so
// prose verbose (config clusters, ticket expiry) falls back to the plain text block.
func verboseRows(v string) ([]render.StatusRow, bool) {
	var rows []render.StatusRow
	for _, ln := range strings.Split(v, "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) < 2 {
			return nil, false
		}
		rows = append(rows, render.StatusRow{
			Level:  f[0],
			Name:   f[1],
			Detail: strings.TrimSpace(strings.Join(f[2:], " ")),
		})
	}
	if len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

func levelStr(s doctor.Status) string {
	switch s {
	case doctor.OK:
		return "ok"
	case doctor.Warn:
		return "warn"
	default:
		return "error"
	}
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func tally(rs []doctor.Result) (ok, warn, fail int) {
	for _, r := range rs {
		switch r.Status {
		case doctor.OK:
			ok++
		case doctor.Warn:
			warn++
		default:
			fail++
		}
	}
	return ok, warn, fail
}
