package cli

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
)

// A batch job's worst-case charge is its walltime times every core it holds for the
// whole run: core-hours = walltime × nodes × cores/node. mu estimates this from the
// script's own scheduler directives — the script is where a project submit declares
// them — best-effort, with a --hours override standing in when the script can't be
// read or parsed. The estimate is deliberately a CEILING (full-node occupancy, the
// whole requested walltime), so a warning it raises is never an under-count.

// reScriptNodes finds a job's node count in either scheduler's directives: PBS
// `-l select=N` / `-l nodes=N`, SLURM `--nodes=N` / `-N N`.
var reScriptNodes = regexp.MustCompile(`(?m)^\s*#\s*(?:PBS\s+-l\s+(?:select|nodes)=|SBATCH\s+(?:-N|--nodes=)\s*)(\d+)`)

// reScriptCores finds cores-per-node when a directive spells it out: PBS select
// `ncpus=M` / `mpiprocs=M`, SLURM `--ntasks-per-node=M`. Absent → the caller falls
// back to the cluster's configured cores/node.
var reScriptCores = regexp.MustCompile(`(?:ncpus=|mpiprocs=|--ntasks-per-node=\s*)(\d+)`)

// estimateCoreHours reads script and returns its worst-case core-hour cost with a
// human basis ("24:00:00 × 4 node(s) × 128 core(s)"). ok=false when the estimate
// can't be trusted — the script is unreadable (it lives on the cluster), declares no
// walltime, or leaves cores/node unknown with none configured for the node — and the
// caller then leans on --hours. A missing node count is the one soft default: 1.
func estimateCoreHours(script, node string) (hours float64, basis string, ok bool) {
	b, err := os.ReadFile(script)
	if err != nil {
		return 0, "", false
	}
	m := reScriptWalltime.FindSubmatch(b)
	if m == nil {
		return 0, "", false
	}
	wsec, wok := queue.ParseWalltime(strings.TrimSpace(string(m[1])))
	if !wok {
		return 0, "", false
	}
	nodes := 1
	if nm := reScriptNodes.FindSubmatch(b); nm != nil {
		if n, e := strconv.Atoi(string(nm[1])); e == nil && n > 0 {
			nodes = n
		}
	}
	cores := 0
	if cm := reScriptCores.FindSubmatch(b); cm != nil {
		cores, _ = strconv.Atoi(string(cm[1]))
	}
	if cores == 0 {
		cores = config.CoresPerNodeFor(node)
	}
	if cores == 0 {
		return 0, "", false
	}
	hours = float64(wsec) / 3600 * float64(nodes) * float64(cores)
	basis = fmt.Sprintf("%s × %d node(s) × %d core(s)", queue.FormatWalltime(wsec), nodes, cores)
	return hours, basis, true
}
