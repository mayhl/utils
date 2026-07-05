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

			// One table per section, sections in first-seen order.
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

			if verbose {
				for _, r := range results {
					if r.Verbose == "" {
						continue
					}
					fmt.Printf("\n%s:\n", r.Name)
					for _, ln := range strings.Split(r.Verbose, "\n") {
						fmt.Printf("  %s\n", ln)
					}
				}
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
	return c
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
