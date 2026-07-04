package render

import (
	"fmt"
	"os"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner is a minimal braille spinner on stderr, for wrapping a blocking op
// (e.g. an sshfs mount settling) with a live "…working" line. Like the progress
// bar it draws only to a TTY and only human-facing (stderr). A no-op off-TTY.
type Spinner struct {
	msg  string
	tty  bool
	stop chan struct{}
	done chan struct{}
}

// NewSpinner creates a spinner with the given message.
func NewSpinner(msg string) *Spinner {
	info, err := os.Stderr.Stat()
	tty := err == nil && info.Mode()&os.ModeCharDevice != 0
	return &Spinner{msg: msg, tty: tty, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start begins animating in a goroutine; pair with Stop.
func (s *Spinner) Start() {
	if !s.tty {
		return
	}
	go func() {
		defer close(s.done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			frame := spinFrames[i%len(spinFrames)]
			if !colorOff() {
				frame = text.Colors{text.FgCyan}.Sprint(frame)
			}
			fmt.Fprintf(os.Stderr, "\r%s %s\033[K", frame, s.msg)
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
