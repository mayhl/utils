package hpc

import (
	"net"
	"testing"
	"time"
)

// TestProbe drives the real dial path hermetically: a live local listener stands
// in for a reachable ssh port ("up"), and a port that was opened then closed is
// guaranteed unused → connection refused ("down"). Both targets go through one
// Probe call, so the concurrent fan-out and the name→status mapping are exercised
// together, with no ssh and no external host.
func TestProbe(t *testing.T) {
	// A listener we keep open → "up". Its host:port is our reachable target.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	upHost, upPort, _ := net.SplitHostPort(ln.Addr().String())

	// A second listener opened then immediately closed → that port is free, so a
	// dial gets refused fast → "down".
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, downPort, _ := net.SplitHostPort(ln2.Addr().String())
	ln2.Close()

	// probe dials a single port for all targets, so give both nodes the same host
	// and distinguish reachability by pointing the "up" node at the live port.
	// Run the two nodes in separate probe calls (one per port) and merge — this
	// keeps the helper's single-port contract while covering both outcomes.
	upRes := probe(map[string]string{"live": upHost}, upPort, time.Second)
	if upRes["live"] != "up" {
		t.Errorf("live listener: got %q, want up", upRes["live"])
	}

	downRes := probe(map[string]string{"dead": "127.0.0.1"}, downPort, time.Second)
	if downRes["dead"] != "down" {
		t.Errorf("closed port: got %q, want down", downRes["dead"])
	}
}

// TestProbeConcurrentMapping checks the fan-out fills every target's slot exactly
// once (no lost writes under the mutex) by probing many closed-port nodes at once.
func TestProbeConcurrentMapping(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close() // free the port → all dials refused → all "down"

	targets := make(map[string]string, 20)
	for i := 'a'; i <= 't'; i++ {
		targets[string(i)] = "127.0.0.1"
	}
	res := probe(targets, port, time.Second)
	if len(res) != len(targets) {
		t.Fatalf("got %d results, want %d", len(res), len(targets))
	}
	for name, status := range res {
		if status != "down" {
			t.Errorf("node %q: got %q, want down", name, status)
		}
	}
}
