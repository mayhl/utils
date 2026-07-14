package render

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
)

var (
	spinFrames      = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinFramesASCII = []string{"|", "/", "-", "\\"}
)

// spinnerFrames picks the frame set: braille reads as mojibake on a non-UTF-8 terminal
// (PuTTY on the clusters), where the old barber-pole is what a spinner is supposed to
// look like anyway.
func spinnerFrames() []string {
	if asciiMode() {
		return spinFramesASCII
	}
	return spinFrames
}

// Spinner is a minimal braille spinner on stderr, for wrapping a blocking op
// (e.g. an sshfs mount settling) with a live "…working" line. Like the progress
// bar it draws only to a TTY and only human-facing (stderr). A no-op off-TTY.
// The message can be updated live (thread-safe) — e.g. a running N/M count while
// a concurrent fan-out completes.
type Spinner struct {
	mu   sync.Mutex
	msg  string
	tty  bool
	stop chan struct{}
	done chan struct{}
}

// SetMessage updates the spinner's line; safe to call while it animates.
func (s *Spinner) SetMessage(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// NewSpinner creates a spinner with the given message.
func NewSpinner(msg string) *Spinner {
	info, err := os.Stderr.Stat()
	tty := err == nil && info.Mode()&os.ModeCharDevice != 0
	return &Spinner{msg: msg, tty: tty, stop: make(chan struct{}), done: make(chan struct{})}
}

// Animating reports whether this spinner will actually draw (stderr is a TTY) — a caller
// that must say SOMETHING falls back to a plain line when it won't.
func (s *Spinner) Animating() bool { return s.tty }

// Start begins animating in a goroutine; pair with Stop.
func (s *Spinner) Start() {
	if !s.tty {
		return
	}
	go func() {
		defer close(s.done)
		frames := spinnerFrames()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			frame := frames[i%len(frames)]
			if !colorOff() {
				frame = text.Colors{text.FgCyan}.Sprint(frame)
			}
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			// Trim to the terminal: the glyph + its gap cost 2 columns, and a message
			// that wraps would leave the tail behind on Stop (\r + \033[K only clear the
			// last line). Keep the head — a spinner message leads with what it's doing.
			if tw := termWidth(); tw > 2 {
				msg = truncRight(msg, tw-2)
			}
			fmt.Fprintf(os.Stderr, "\r%s %s\033[K", frame, msg)
			select {
			case <-s.stop:
				fmt.Fprint(os.Stderr, "\r\033[K")
				return
			case <-tick.C:
			}
		}
	}()
}

// Stop halts the animation and clears the line.
func (s *Spinner) Stop() {
	if !s.tty {
		return
	}
	close(s.stop)
	<-s.done
}
