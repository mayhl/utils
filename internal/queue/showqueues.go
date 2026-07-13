package queue

import "strings"

// QueueInfo is one row of `show_queues` QUEUE INFORMATION: a batch queue's limits, live
// counts, and type/enabled/running flags. Fields stay strings so unlimited/blank limits,
// non-numeric markers, and the blank E/R flags some systems emit survive verbatim;
// callers convert as needed. Type (Exe/Rou) distinguishes submittable from routing queues.
type QueueInfo struct {
	System      string // owning cluster, tagged by the collate views (show_queues doesn't emit it)
	Name        string
	MaxWalltime string // HH:MM:SS, "" if blank
	MaxJobs     string
	MinCores    string
	MaxCores    string
	JobsRun     string
	JobsPend    string
	CoresRun    string
	CoresPend   string
	Type        string // Typ
	Enabled     string // E
	Running     string // R
}

// ParseShowQueues parses the QUEUE INFORMATION table from `show_queues` (same wide format
// on PBS and SLURM). It anchors on the `===` ruler (so the multi-line header, leading
// blanks, and column-width drift don't matter), reads rows until a blank line / the NODE
// INFORMATION section / EOF, and splits the final "Typ E R" column into its whitespace-
// separated fields — tolerating rows that carry only Type when E/R are blank. NODE
// INFORMATION is not parsed here.
func ParseShowQueues(text string) []QueueInfo {
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "QUEUE INFORMATION") {
		i++
	}
	for i < len(lines) && !isRuler(lines[i]) {
		i++
	}
	if i >= len(lines) {
		return nil
	}
	starts := columnStarts(lines[i])
	if len(starts) < 10 {
		return nil
	}
	var rows []QueueInfo
	for i++; i < len(lines); i++ {
		ln := lines[i]
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(strings.TrimSpace(ln), "NODE INFORMATION") {
			break
		}
		c := sliceCols(ln, starts)
		flags := strings.Fields(c[9]) // "Typ E R" → up to 3 fields
		rows = append(rows, QueueInfo{
			Name: c[0], MaxWalltime: c[1], MaxJobs: c[2], MinCores: c[3], MaxCores: c[4],
			JobsRun: c[5], JobsPend: c[6], CoresRun: c[7], CoresPend: c[8],
			Type: field(flags, 0), Enabled: field(flags, 1), Running: field(flags, 2),
		})
	}
	return rows
}

// isRuler reports whether a line is a `=`-and-space column underline.
func isRuler(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '=' && r != ' ' {
			return false
		}
	}
	return true
}

// columnStarts returns the byte offset where each run of `=` begins — one per column.
func columnStarts(ruler string) []int {
	var starts []int
	inRun := false
	for i := 0; i < len(ruler); i++ {
		if ruler[i] == '=' {
			if !inRun {
				starts = append(starts, i)
				inRun = true
			}
		} else {
			inRun = false
		}
	}
	return starts
}

// sliceCols cuts a data line at the column starts: column j spans [starts[j], starts[j+1]),
// the last runs to EOL; each cell is trimmed. Columns past a short line's end yield "".
func sliceCols(line string, starts []int) []string {
	cells := make([]string, len(starts))
	for j, s := range starts {
		if s >= len(line) {
			continue
		}
		end := len(line)
		if j+1 < len(starts) && starts[j+1] < end {
			end = starts[j+1]
		}
		cells[j] = strings.TrimSpace(line[s:end])
	}
	return cells
}

func field(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}
