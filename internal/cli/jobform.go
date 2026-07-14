package cli

import (
	"fmt"
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

// Field indices of the tunnel form (`mu job tunnel -i`) and the shell form
// (`mu job shell -i`) — each form's own numbering, like the sub form's.
const (
	tfScript = iota
	tfJob
	tfQueue
	tfAccount
	tfWalltime
	tfPort
	tfLocal
)

const (
	shQueue = iota
	shAccount
	shWalltime
)

// subForm runs the `mu job sub -i` form and maps its values to SubmitOpts. Fields are
// pre-seeded from the flags and the cluster's config; a Load fetch enriches the queue
// enum with the live queue list and arms the walltime/nodes limit validation once it
// lands. ok=false means the user cancelled. The form only gathers — the caller runs
// the usual preview + confirm + submit.
func subForm(node, label, script, account string, sel *queueSel, walltime string, nodes int, name string) (string, queue.SubmitOpts, bool, error) {
	queueVal, pendingKey, options := queueSeed(label, sel, true)

	// --debug means "give me the whole slot", so the field opens on the queue's maximum —
	// same rule as the flag path, or -i would quietly submit a different job than the flags
	// alone would. The script still wins if it declares one (the caller checks).
	if walltime == "" && (sel.debug || sel.dbg) && mayInjectWalltime(script) {
		if max, ok := queueMax(node, queueVal); ok {
			walltime = queue.FormatWalltime(max)
		}
	}
	nodesVal := ""
	if nodes > 0 {
		nodesVal = strconv.Itoa(nodes)
	}
	fields := []render.FormField{
		{Label: "script", Value: script, Hint: "path resolved on " + label, Validate: requiredField},
		{Label: "queue", Value: queueVal, Kind: render.FieldEnum, Options: options},
		{Label: "account", Value: account},
		{Label: "walltime", Value: walltime, Hint: wallHint, Validate: walltimeField},
		{Label: "nodes", Value: nodesVal, Validate: intField},
		{Label: "name", Value: name},
	}
	vals, ok, err := render.Form(render.FormSpec{
		Title:  "Submit to " + label,
		Fields: fields,
		Load: func() []render.FieldPatch {
			return queuePatches(node, label, pendingKey, queueFields{queue: sfQueue, walltime: sfWalltime, nodes: sfNodes})
		},
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
	// The field takes a duration ("1.5h"); the scheduler takes H+:MM:SS. Normalize on the
	// way out, against the queue the user actually settled on.
	wall, err := resolveWalltime(node, q, vals[sfWalltime], "", false)
	if err != nil {
		return "", queue.SubmitOpts{}, false, err
	}
	part, qos := submitTarget(label, q)
	opts := queue.SubmitOpts{
		Account: vals[sfAccount], Queue: part, QOS: qos,
		Walltime: wall, Nodes: n, Name: vals[sfName],
		CoresPerNode: queueCPN(label, q),
	}
	return vals[sfScript], opts, true, nil
}

// queueSeed builds the queue field's initial value and enum options from the flags and
// config alone (no fetch — the list arrives via the Load patch). The value: -q literal, a
// class flag via config (or its standard literal), else the cluster's bare default; a class
// flag with NO config entry stays pending — the Load patch selects the single class match.
// Options: the sentinel + the seed + every configured submit_queue entry, deduped.
// bareDefault mirrors queueSel.resolve: only `sub` seeds a flagless form with
// submit_queue.default; tunnel/shell start on the scheduler default.
func queueSeed(label string, sel *queueSel, bareDefault bool) (queueVal, pendingKey string, options []string) {
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
	case bareDefault:
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

// queueFields tells queuePatches which of a form's fields the queue list feeds. Each form
// numbers its own fields, and the shell/tunnel forms have no walltime or nodes — an absent
// field is -1 and gets no patch.
type queueFields struct{ queue, walltime, nodes int }

// queuePatches is the queue-backed forms' Load: read the queue list QUIETLY (cachedQueues
// renders nothing — we're under the TUI; the ticket was ensured before the form opened) and
// turn it into patches: real names for the queue enum, the pending class flag's single match
// selected, and walltime/nodes (where the form has them) validated against the selected
// queue's limits. A failed read returns nil — the form just stays on its config seed.
func queuePatches(node, label, pendingKey string, ix queueFields) []render.FieldPatch {
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
	qPatch := render.FieldPatch{Index: ix.queue, Options: names}
	if pendingKey != "" {
		if match := classQueues(label, submitClasses[pendingKey], qs); len(match) == 1 {
			qPatch.Value = match[0]
		}
	}
	patches := []render.FieldPatch{qPatch}
	if ix.walltime >= 0 {
		patches = append(patches, render.FieldPatch{Index: ix.walltime, Validate: func(v string, all []string) string {
			if msg := walltimeField(v, all); msg != "" || v == "" {
				return msg
			}
			q, ok := limits[all[ix.queue]]
			if !ok || q.MaxWalltime == "" {
				return ""
			}
			// Compare in SECONDS, so "1.5h" can't slip past a limit written 01:00:00.
			want, _ := queue.ParseWalltime(v)
			if max, ok := queue.ParseWalltime(q.MaxWalltime); ok && want > max {
				return "over the " + all[ix.queue] + " max " + q.MaxWalltime
			}
			return ""
		}})
	}
	if ix.nodes >= 0 {
		patches = append(patches, render.FieldPatch{Index: ix.nodes, Validate: func(v string, all []string) string {
			if msg := intField(v, all); msg != "" || v == "" {
				return msg
			}
			q, ok := limits[all[ix.queue]]
			if !ok {
				return ""
			}
			maxN := queueMaxNodes(label, q.Name, q.MaxCores)
			if maxN == "" {
				return ""
			}
			n, _ := strconv.Atoi(v)
			if mx, err := strconv.Atoi(maxN); err == nil && n > mx {
				return fmt.Sprintf("over the %s max %s nodes", all[ix.queue], maxN)
			}
			return ""
		}})
	}
	return patches
}

// tunnelFields is what `mu job tunnel -i` gathers: the same knobs the flags set. Queue ""
// means the scheduler default (the form's sentinel), as with the flags.
type tunnelFields struct {
	Script, JobID, Account, Queue, Walltime string
	Port, LocalPort                         int
}

// tunnelForm runs the `mu job tunnel -i` form. Fields are pre-seeded from the flags and
// config, and the queue enum is enriched by the same Load as the sub form. The script /
// --job exclusivity that the flag path checks in RunE is a cross-field rule here, so the
// form itself refuses to submit until exactly one of them is set. Gathers only — the
// caller runs the usual preview + confirm and holds the connection.
func tunnelForm(node, label, script, jobID, account, walltime string, sel *queueSel, port, localPort int) (tunnelFields, bool, error) {
	queueVal, pendingKey, options := queueSeed(label, sel, false)
	walltime = seedWalltime(node, label, script, walltime, queueVal, sel)
	fields := []render.FormField{
		{Label: "script", Value: script, Hint: "the service to submit, path resolved on " + label, Validate: eitherScriptOrJob},
		{Label: "job", Value: jobID, Hint: "adopt an already-submitted job instead", Validate: eitherScriptOrJob},
		{Label: "queue", Value: queueVal, Kind: render.FieldEnum, Options: options},
		{Label: "account", Value: account},
		{Label: "walltime", Value: walltime, Hint: wallHint, Validate: walltimeField},
		{Label: "port", Value: intOrBlank(port), Hint: "service port ON the compute node", Validate: requiredPort},
		{Label: "local", Value: intOrBlank(localPort), Hint: "local port to listen on (blank: same)", Validate: intField},
	}
	vals, ok, err := render.Form(render.FormSpec{
		Title:  "Tunnel to " + label,
		Fields: fields,
		Load: func() []render.FieldPatch {
			return queuePatches(node, label, pendingKey, queueFields{queue: tfQueue, walltime: tfWalltime, nodes: -1})
		},
		LoadNote: "fetching queues...",
	})
	if err != nil || !ok {
		return tunnelFields{}, false, err
	}
	out := tunnelFields{
		Script: vals[tfScript], JobID: vals[tfJob], Account: vals[tfAccount],
		Queue: vals[tfQueue], Walltime: vals[tfWalltime],
		Port: atoiOr(vals[tfPort], 0), LocalPort: atoiOr(vals[tfLocal], 0),
	}
	if out.Queue == schedDefault {
		out.Queue = ""
	}
	if out.LocalPort == 0 {
		out.LocalPort = out.Port
	}
	return out, true, nil
}

// shellForm runs the `mu job shell -i` form: the two knobs an interactive allocation takes,
// with the queue enum backed by the live list — which is the point, since the class flags
// can't name a queue you didn't know existed.
func shellForm(node, label, account, walltime string, sel *queueSel) (queueName, acct, wall string, ok bool, err error) {
	queueVal, pendingKey, options := queueSeed(label, sel, false)
	walltime = seedWalltime(node, label, "", walltime, queueVal, sel)
	vals, ok, err := render.Form(render.FormSpec{
		Title: "Interactive allocation on " + label,
		Fields: []render.FormField{
			{Label: "queue", Value: queueVal, Kind: render.FieldEnum, Options: options},
			{Label: "account", Value: account},
			{Label: "walltime", Value: walltime, Hint: wallHint, Validate: walltimeField},
		},
		Load: func() []render.FieldPatch {
			return queuePatches(node, label, pendingKey, queueFields{queue: shQueue, walltime: shWalltime, nodes: -1})
		},
		LoadNote: "fetching queues...",
	})
	if err != nil || !ok {
		return "", "", "", false, err
	}
	queueName = vals[shQueue]
	if queueName == schedDefault {
		queueName = ""
	}
	return queueName, vals[shAccount], vals[shWalltime], true, nil
}

// seedWalltime is what a held session's walltime field OPENS on: an explicit -t, else the
// queue's whole slot under --debug, else the config default — the same order the flag path
// resolves, so -i and the flags can't disagree about what they'd submit. Left as typed (not
// normalized): the field is the user's, and it's normalized on the way out.
func seedWalltime(node, label, script, walltime, queueVal string, sel *queueSel) string {
	if walltime != "" || !mayInjectWalltime(script) {
		return walltime
	}
	if sel.debug || sel.dbg {
		if max, ok := queueMax(node, queueVal); ok {
			return queue.FormatWalltime(max)
		}
	}
	dflt, err := interactiveWalltime(label)
	if err != nil {
		return "" // a bad config value is reported by the caller's own resolve
	}
	return dflt
}

// eitherScriptOrJob is the tunnel form's cross-field rule, on both fields so the message
// lands wherever the cursor is: submit a script OR adopt a job id, never both, never neither.
func eitherScriptOrJob(_ string, all []string) string {
	switch {
	case all[tfScript] != "" && all[tfJob] != "":
		return "script and job are exclusive — submit one or adopt the other"
	case all[tfScript] == "" && all[tfJob] == "":
		return "need a script to submit, or a job id to adopt"
	}
	return ""
}

// requiredPort is the tunnel's one non-optional number: without it there is nothing to
// forward.
func requiredPort(v string, all []string) string {
	if v == "" {
		return "the service port on the compute node"
	}
	return intField(v, all)
}

// intOrBlank renders a flag's int back into a form field: 0 (unset) shows as empty, not "0".
func intOrBlank(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// atoiOr parses a validated int field, falling back for the empty/unset case.
func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}

// wallHint is what the walltime field advertises — the shorthand, since that's the whole
// point of accepting it.
const wallHint = "HH:MM:SS or 10m / 1h / 1.5h"

// walltimeField accepts empty (fall through to the script / the scheduler) or anything
// queue.ParseWalltime reads — canonical or shorthand.
func walltimeField(v string, _ []string) string {
	if v == "" {
		return ""
	}
	if _, ok := queue.ParseWalltime(v); !ok {
		return "want " + wallHint
	}
	return ""
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
