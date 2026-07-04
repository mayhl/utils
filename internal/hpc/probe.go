package hpc

import (
	"net"
	"sync"
	"time"
)

// Probe checks reachability of each node concurrently by TCP-dialing its ssh port
// (22) — not ICMP ping, which DoD login nodes commonly block and which needs raw
// sockets. It answers the question that matters ("can I ssh?") and, being
// concurrent, completes in ~one timeout rather than N. Returns node name →
// "up" | "down". The map is targets: node name → host (FQDN).
func Probe(targets map[string]string, timeout time.Duration) map[string]string {
	out := make(map[string]string, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, host := range targets {
		wg.Add(1)
		go func(name, host string) {
			defer wg.Done()
			status := "down"
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "22"), timeout)
			if err == nil {
				_ = conn.Close()
				status = "up"
			}
			mu.Lock()
			out[name] = status
			mu.Unlock()
		}(name, host)
	}
	wg.Wait()
	return out
}
