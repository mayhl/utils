package cli

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/render"
)

// The interactive allocation's narration.
//
// `ssh -t <login> salloc …` carries the WHOLE session down one pty, so the cluster's MOTD,
// its dbus/X11 profile noise, salloc's six-line progress report and the shell you actually
// asked for all arrive on the same stream. No ssh flag separates them — the pty is one
// channel by construction — so mu reads the stream and decides. (The pre-auth SSH banner is
// the exception: the client prints that one, and `ssh -q` silences it.)
//
// Three phases:
//
//  1. HOLD  — everything before salloc first speaks is connect-time preamble: MOTD, news,
//     profile chatter. Held, and DISCARDED once salloc speaks. HELD rather than dropped
//     outright so a session that dies before salloc ever runs — refused key, dead host, bad
//     account — still prints why. Silence is the one thing this must never produce.
//  2. ALLOC — salloc's own lines, re-rendered as house lines: six become three.
//  3. PASS  — the shell is the user's; everything through verbatim, bar the known noise.
type allocView struct {
	out   io.Writer
	buf   []byte // the partial line still arriving
	held  []byte // phase 1, pending discard-or-flush
	phase int
	noise *regexp.Regexp
	jobID string
	said  bool // the "queued" line is raised once, not per salloc phrasing of it

	spin *render.Spinner
	tick chan struct{} // stops the elapsed-time updater
}

const (
	phaseHold = iota
	phaseAlloc
	phasePass
)

func newAllocView(out io.Writer) *allocView {
	return &allocView{out: out, noise: hpc.StderrNoise()}
}

func (a *allocView) Write(p []byte) (int, error) {
	n := len(p)
	a.buf = append(a.buf, p...)
	for {
		i := bytes.IndexByte(a.buf, '\n')
		if i < 0 {
			break
		}
		line := string(a.buf[:i])
		a.buf = a.buf[i+1:]
		a.line(line)
	}
	// A partial line must NOT wait for a newline that isn't coming: the SHELL PROMPT is
	// exactly such a line, and holding it back would read as a hang at the moment the
	// session finally becomes usable. But a partial arriving mid-allocation might still be
	// the start of a salloc line ("sall" + "oc: Granted…"), so it is released only once it
	// can no longer become one.
	if len(a.buf) > 0 && a.phase != phaseHold && !a.maybeSalloc() {
		a.phase = phasePass
		_, err := a.out.Write(a.buf)
		a.buf = a.buf[:0]
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// maybeSalloc reports whether the partial line could still turn out to be salloc's.
func (a *allocView) maybeSalloc() bool {
	b := bytes.TrimLeft(a.buf, " \t")
	const tag = "salloc:"
	if len(b) < len(tag) {
		return bytes.HasPrefix([]byte(tag), b)
	}
	return bytes.HasPrefix(b, []byte(tag))
}

func (a *allocView) line(s string) {
	text := strings.TrimRight(s, "\r")
	if msg, ok := strings.CutPrefix(strings.TrimSpace(text), "salloc:"); ok {
		a.phase, a.held = phaseAlloc, nil // salloc spoke — the preamble was noise after all
		a.salloc(strings.TrimSpace(msg))
		return
	}
	switch a.phase {
	case phaseHold:
		a.held = append(append(a.held, text...), '\n')
	default:
		a.phase = phasePass // salloc has stopped talking: this is the shell
		a.stopWait()
		if a.noise != nil && a.noise.MatchString(text) {
			return
		}
		_, _ = io.WriteString(a.out, s+"\n")
	}
}

var (
	reAllocJob   = regexp.MustCompile(`job (?:allocation )?(\d+)`)
	reAllocNodes = regexp.MustCompile(`^Nodes (\S+) are ready`)
)

// salloc re-renders one of salloc's lines. Its six-line report says three things — queued,
// configuring, ready — so that is what mu prints. Anything it says that mu does not
// recognize is passed through rather than swallowed: an unknown salloc message is far more
// likely to matter than to be noise.
func (a *allocView) salloc(msg string) {
	if id := reAllocJob.FindStringSubmatch(msg); id != nil {
		a.jobID = id[1]
	}
	switch {
	case strings.HasPrefix(msg, "error:"):
		render.Err("salloc: " + strings.TrimSpace(strings.TrimPrefix(msg, "error:")))
	case strings.Contains(msg, "Pending job allocation"), strings.Contains(msg, "queued and waiting"):
		if !a.said {
			a.said = true
			a.wait("job " + a.jobID + " queued — waiting for resources")
		}
	case strings.Contains(msg, "Granted job allocation"), strings.Contains(msg, "has been allocated resources"):
		// Both say the same thing, and the next line says it better.
	case strings.Contains(msg, "Waiting for resource configuration"):
		a.stopWait()
		render.Detail("configuring nodes...")
	case reAllocNodes.MatchString(msg):
		a.stopWait()
		render.OK(reAllocNodes.FindStringSubmatch(msg)[1] + " ready — job " + a.jobID)
	default:
		a.stopWait()
		render.Detail("salloc: " + msg)
	}
}

// wait spins while the job sits in the queue, counting UP. A spinner alone says "still
// alive"; the elapsed time is the thing you actually want, because a queue wait has no
// upper bound and the only question worth asking is whether to keep waiting.
func (a *allocView) wait(msg string) {
	a.spin = render.NewSpinner(msg)
	if !a.spin.Animating() { // piped or redirected — say it once, plainly
		a.spin = nil
		render.Info(msg)
		return
	}
	a.spin.Start()
	a.tick = make(chan struct{})
	go func(start time.Time, stop <-chan struct{}, spin *render.Spinner) {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				d := now.Sub(start).Round(time.Second)
				spin.SetMessage(fmt.Sprintf("%s (%dm%02ds)", msg, int(d.Minutes()), int(d.Seconds())%60))
			}
		}
	}(time.Now(), a.tick, a.spin)
}

// stopWait clears the spinner before anything else prints — its line is redrawn in place,
// so a house line written over it would land in the middle of the animation.
func (a *allocView) stopWait() {
	if a.spin == nil {
		return
	}
	close(a.tick)
	a.spin.Stop()
	a.spin, a.tick = nil, nil
}

// flush ends the stream. A session that never reached salloc still owes the user whatever it
// printed on the way down — that held text is the only account of what went wrong.
func (a *allocView) flush() {
	a.stopWait()
	if len(a.buf) > 0 && a.phase != phaseHold {
		_, _ = a.out.Write(a.buf)
	}
	if a.phase == phaseHold && len(bytes.TrimSpace(a.held)) > 0 {
		_, _ = a.out.Write(a.held)
	}
	a.buf, a.held = nil, nil
}
