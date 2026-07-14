package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// How long a job asks for.
//
// Three sources, in order: an explicit -t; --debug, which means "give me the whole slot" and
// so asks for that queue's maximum; and — for an INTERACTIVE session only — the config
// default. Whatever comes out is clamped to the queue's own maximum, which the cached queue
// inventory already knows, so a 1h default on a machine whose debug queue caps at 30 minutes
// becomes 30 minutes instead of a rejected submit. No cache, no clamp: mu sends the request
// as-is and lets the scheduler answer, the same honest degradation as the account picker.

// queueMax is the selected queue's walltime limit in seconds, from the cached inventory.
// ok=false when nothing is cached for the machine, or the center leaves the limit blank.
func queueMax(node, queueName string) (int, bool) {
	if queueName == "" {
		return 0, false
	}
	_, qs, err := cachedQueues(node)
	if err != nil {
		return 0, false // a walltime is no reason to fail a command over a queue listing
	}
	for _, q := range qs {
		if q.Name == queueName {
			return queue.ParseWalltime(q.MaxWalltime)
		}
	}
	return 0, false
}

// resolveWalltime picks and normalizes the walltime for a submission. want is the -t value
// (may be empty); dflt is the fallback when -t is silent — the config default for an
// interactive session, empty for a batch job, whose script speaks for itself. debug asks for
// the queue's whole slot. The result is canonical H+:MM:SS, or "" for "send no walltime".
func resolveWalltime(node, queueName, want, dflt string, debug bool) (string, error) {
	req := want
	if req == "" && debug {
		if max, ok := queueMax(node, queueName); ok {
			req = queue.FormatWalltime(max)
		}
	}
	if req == "" {
		req = dflt
	}
	norm, ok := queue.NormalizeWalltime(req)
	if !ok {
		return "", usageErr("walltime %q: want HH:MM:SS or a duration (10m, 1h, 1.5h, 1h30m)", req)
	}
	return clampWalltime(node, queueName, norm), nil
}

// clampWalltime cuts a walltime down to the queue's maximum, saying so — the number you are
// told is the number that gets submitted. Silent clamping would be worse than none: you'd
// plan around a walltime the scheduler never gave you.
func clampWalltime(node, queueName, wall string) string {
	if wall == "" {
		return ""
	}
	max, ok := queueMax(node, queueName)
	if !ok {
		return wall
	}
	sec, ok := queue.ParseWalltime(wall)
	if !ok || sec <= max {
		return wall
	}
	capped := queue.FormatWalltime(max)
	render.Info(fmt.Sprintf("walltime %s → %s (%s's maximum)", wall, capped, queueName))
	return capped
}

// interactiveWalltime is the config default for a held session (`mu job shell`, `mu job
// tunnel`), validated here so a typo in config.toml is reported once, by name, rather than
// as a scheduler rejection minutes later.
func interactiveWalltime(label string) (string, error) {
	v := config.InteractiveWalltimeFor(label)
	if v == "" {
		return "", nil
	}
	if _, ok := queue.NormalizeWalltime(v); !ok {
		return "", runErr("config interactive_walltime = %q: want HH:MM:SS or a duration (10m, 1h, 1.5h)", v)
	}
	return v, nil
}

// reScriptWalltime finds a walltime a job script already declares for itself.
var reScriptWalltime = regexp.MustCompile(`(?m)^\s*#\s*(?:PBS\s+-l\s+walltime=|SBATCH\s+(?:-t|--time=)\s*)(\S+)`)

// warnScriptWalltime checks a --debug submission against what the script asks for.
//
// mu does NOT rewrite it: a qsub `-l walltime=` OVERRIDES the script's own directive, so
// injecting the queue maximum would silently change the meaning of a script that already
// says what it needs — and the script is the one that knows. But since the cache knows the
// cap, mu can at least say the submission is doomed before the scheduler does. A script mu
// can't read (it lives on the cluster) is simply not checked.
func warnScriptWalltime(node, queueName, script string) {
	b, err := os.ReadFile(script)
	if err != nil {
		return
	}
	m := reScriptWalltime.FindSubmatch(b)
	if m == nil {
		return
	}
	asks, ok := queue.ParseWalltime(strings.TrimSpace(string(m[1])))
	if !ok {
		return
	}
	max, ok := queueMax(node, queueName)
	if !ok || asks <= max {
		return
	}
	render.Warn(fmt.Sprintf("the script asks for %s, but %s caps at %s — the scheduler will refuse it (override with -t)",
		queue.FormatWalltime(asks), queueName, queue.FormatWalltime(max)))
}

// mayInjectWalltime reports whether mu may supply a walltime of its own — the --debug
// maximum or the interactive default. Only when the script is SILENT about its own.
//
// A script mu cannot read counts as declaring one, which is the common case: the path is
// resolved on the cluster, not here. That asymmetry is deliberate — a qsub `-l walltime=`
// overrides the script's directive, and overriding one you never saw is how a 24-hour run
// silently becomes a 30-minute one. An explicit -t is always honoured; this gates only the
// defaults, so the escape hatch costs one flag.
func mayInjectWalltime(script string) bool {
	if strings.TrimSpace(script) == "" {
		return true // no script at all (an interactive session) — nothing to override
	}
	b, err := os.ReadFile(script)
	if err != nil {
		return false
	}
	return !reScriptWalltime.Match(b)
}

// queueTarget puts a resolved queue name into the field the site's SLURM actually reads:
// --qos= where the center implements its queues as QOS values (config queue_flag = "qos"),
// -p everywhere else. Every SubmitOpts mu builds goes through here, so the choice is made
// once rather than at each of the six call sites.
//
// The name mu resolved is the same either way — what changes is only the flag it rides.
func submitTarget(label, queueName string) (queue_, qos string) {
	if queueName == "" {
		return "", ""
	}
	if config.QueueFlagFor(label) == "qos" && config.SchedulerFor(label) == "slurm" {
		return "", queueName
	}
	return queueName, ""
}

// wallLeft is requested-minus-elapsed, as a human duration — the answer to "how much longer
// does this tunnel have". "" when either side didn't parse (the scheduler left a field blank
// or gave a form ParseWalltime doesn't read), so the column degrades rather than lying.
func wallLeft(reqWall, elapsed string) string {
	req, ok1 := queue.ParseWalltime(strings.TrimSpace(reqWall))
	el, ok2 := queue.ParseWalltime(strings.TrimSpace(elapsed))
	if !ok1 || !ok2 {
		return ""
	}
	if el >= req {
		return "0s"
	}
	return queue.FormatWalltime(req - el)
}
