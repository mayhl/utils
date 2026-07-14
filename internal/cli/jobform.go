package cli

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// schedDefault is the queue field's "no -q" sentinel: submit with no queue flag and
// let the script's directives / the scheduler default decide.
const schedDefault = "(scheduler default)"

// Field indices of the sub form — shared by subForm and the Load patches.
const (
	sfScript = iota
	sfQueue
	sfAccount
	sfWalltime
	sfNodes
	sfName
)

// submitKeys is the config lookup order for seeding the queue enum's initial options.
var submitKeys = []string{"default", "gpu", "vis", "bigmem", "xfer", "debug", "background"}

// subForm runs the `mu job sub -i` form and maps its values to SubmitOpts. Fields are
// pre-seeded from the flags and the cluster's config; a Load fetch enriches the queue
// enum with the live queue list and arms the walltime/nodes limit validation once it
// lands. ok=false means the user cancelled. The form only gathers — the caller runs
// the usual preview + confirm + submit.
func subForm(node, label, script, account string, sel *queueSel, walltime string, nodes int, name string) (string, queue.SubmitOpts, bool, error) {
	queueVal, pendingKey, options := subFormSeed(label, sel)

	nodesVal := ""
	if nodes > 0 {
		nodesVal = strconv.Itoa(nodes)
	}
	fields := []render.FormField{
		{Label: "script", Value: script, Hint: "path resolved on " + label, Validate: requiredField},
		{Label: "queue", Value: queueVal, Kind: render.FieldEnum, Options: options},
		{Label: "account", Value: account},
		{Label: "walltime", Value: walltime, Hint: "HH:MM:SS", Validate: walltimeField},
		{Label: "nodes", Value: nodesVal, Validate: intField},
		{Label: "name", Value: name},
	}
	vals, ok, err := render.Form(render.FormSpec{
		Title:    "Submit to " + label,
		Fields:   fields,
		Load:     func() []render.FieldPatch { return subFormPatches(node, label, pendingKey) },
		LoadNote: "fetching queues...",
	})
	if err != nil || !ok {
		return "", queue.SubmitOpts{}, false, err
	}
	q := vals[sfQueue]
	if q == schedDefault {
		q = ""
	}
	n := 0
	if vals[sfNodes] != "" {
		n, _ = strconv.Atoi(vals[sfNodes])
	}
	opts := queue.SubmitOpts{
		Account: vals[sfAccount], Queue: q,
		Walltime: vals[sfWalltime], Nodes: n, Name: vals[sfName],
		CoresPerNode: queueCPN(label, q),
	}
	return vals[sfScript], opts, true, nil
}

// subFormSeed builds the queue field's initial value and enum options from the flags
// and config alone (no fetch — the live list arrives via the Load patch). The value:
// -q literal, a class flag via config (or its standard literal), else the cluster's
// bare-sub default; a class flag with NO config entry stays pending — the Load patch
// selects the single live class match. Options: the sentinel + the seed + every
// configured submit_queue entry, deduped.
func subFormSeed(label string, sel *queueSel) (queueVal, pendingKey string, options []string) {
	switch key := sel.key(); {
	case sel.queue != "":
		queueVal = sel.queue
	case key != "":
		if queueVal = config.SubmitQueueFor(label, key); queueVal == "" {
			if q, ok := submitLiterals[key]; ok {
				queueVal = q
			} else {
				pendingKey = key
			}
		}
	default:
		queueVal = config.SubmitQueueFor(label, "default")
	}
	options = []string{schedDefault}
	seen := map[string]bool{schedDefault: true}
	add := func(q string) {
		if q != "" && !seen[q] {
			seen[q] = true
			options = append(options, q)
		}
	}
	add(queueVal)
	for _, k := range submitKeys {
		add(config.SubmitQueueFor(label, k))
	}
	if queueVal == "" {
		queueVal = schedDefault
	}
	return queueVal, pendingKey, options
}

// subFormPatches is the form's Load: read the queue list QUIETLY (cachedQueues renders
// nothing — we're under the TUI; the ticket was ensured before the form opened) and turn
// it into patches: real names for the queue enum, the pending class flag's single match
// selected, and walltime/nodes validated against the selected queue's limits. A failed
// read returns nil — the form just stays on its config seed.
func subFormPatches(node, label, pendingKey string) []render.FieldPatch {
	_, qs, err := cachedQueues(node)
	if err != nil {
		return nil
	}
	up, _ := upQueues(execQueues(qs))
	if len(up) == 0 {
		return nil
	}
	names := []string{schedDefault}
	limits := make(map[string]queue.QueueInfo, len(up))
	for _, q := range up {
		names = append(names, q.Name)
		limits[q.Name] = q
	}
	qPatch := render.FieldPatch{Index: sfQueue, Options: names}
	if pendingKey != "" {
		if match := classQueues(label, submitClasses[pendingKey], qs); len(match) == 1 {
			qPatch.Value = match[0]
		}
	}
	return []render.FieldPatch{
		qPatch,
		{Index: sfWalltime, Validate: func(v string, all []string) string {
			if msg := walltimeField(v, all); msg != "" || v == "" {
				return msg
			}
			q, ok := limits[all[sfQueue]]
			if !ok || q.MaxWalltime == "" {
				return ""
			}
			if wallSeconds(v) > wallSeconds(q.MaxWalltime) {
				return "over the " + all[sfQueue] + " max " + q.MaxWalltime
			}
			return ""
		}},
		{Index: sfNodes, Validate: func(v string, all []string) string {
			if msg := intField(v, all); msg != "" || v == "" {
				return msg
			}
			q, ok := limits[all[sfQueue]]
			if !ok {
				return ""
			}
			maxN := queueMaxNodes(label, q.Name, q.MaxCores)
			if maxN == "" {
				return ""
			}
			n, _ := strconv.Atoi(v)
			if mx, err := strconv.Atoi(maxN); err == nil && n > mx {
				return fmt.Sprintf("over the %s max %s nodes", all[sfQueue], maxN)
			}
			return ""
		}},
	}
}

var wallRe = regexp.MustCompile(`^\d+:[0-5]\d:[0-5]\d$`)

// walltimeField accepts empty (fall through to the script) or H+:MM:SS.
func walltimeField(v string, _ []string) string {
	if v == "" || wallRe.MatchString(v) {
		return ""
	}
	return "want HH:MM:SS"
}

// wallSeconds converts H+:MM:SS to seconds (0 on malformed input — callers validate
// the format first).
func wallSeconds(s string) int {
	p := strings.Split(s, ":")
	if len(p) != 3 {
		return 0
	}
	h, _ := strconv.Atoi(p[0])
	m, _ := strconv.Atoi(p[1])
	sec, _ := strconv.Atoi(p[2])
	return h*3600 + m*60 + sec
}

func requiredField(v string, _ []string) string {
	if strings.TrimSpace(v) == "" {
		return "required"
	}
	return ""
}

// intField accepts empty (unset) or a positive integer.
func intField(v string, _ []string) string {
	if v == "" {
		return ""
	}
	if n, err := strconv.Atoi(v); err != nil || n <= 0 {
		return "want a positive integer"
	}
	return ""
}
