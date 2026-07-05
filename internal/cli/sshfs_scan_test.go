package cli

import "testing"

// The scanner must catch a fatal sshfs line even when it arrives split across writes,
// and hand back just that line (trimmed), so runMount can fail fast and show it.
func TestStderrScannerFatalLine(t *testing.T) {
	w := &stderrScanner{fatal: make(chan string, 1)}
	// sshfs prints the target then the error; simulate a mid-pattern chunk boundary.
	w.Write([]byte("me@host.example.mil:/dummy/path: No such "))
	w.Write([]byte("file or directory\n"))
	select {
	case got := <-w.fatal:
		want := "me@host.example.mil:/dummy/path: No such file or directory"
		if got != want {
			t.Fatalf("fatal line = %q, want %q", got, want)
		}
	default:
		t.Fatal("expected a fatal signal, got none")
	}
}

// Benign stderr (e.g. a login banner) must not trip the fatal path.
func TestStderrScannerIgnoresBenign(t *testing.T) {
	w := &stderrScanner{fatal: make(chan string, 1)}
	w.Write([]byte("Warning: Permanently added 'host' to known hosts.\n"))
	if w.sent {
		t.Fatal("benign line tripped the fatal signal")
	}
}
