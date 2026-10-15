package app

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCloseStopsEverythingStartStarted pins that Close ends every goroutine
// Start spawned even when the caller's context lives on. The bridge hands
// Start the application's lifetime context, which outlives a logout, a wipe
// and an unlink; anything bound only to that context kept polling a closed
// store and holding the old Service in memory for the rest of the process.
func TestCloseStopsEverythingStartStarted(t *testing.T) {
	start := func() *Service {
		svc, err := Start(context.Background(), Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		return svc
	}
	settle := func(want int) int {
		deadline := time.Now().Add(10 * time.Second)
		n := runtime.NumGoroutine()
		for time.Now().Before(deadline) && n > want {
			time.Sleep(50 * time.Millisecond)
			n = runtime.NumGoroutine()
		}
		return n
	}

	// Warm up anything process-wide (libp2p's global workers, the race
	// detector's own) so the baseline is the steady state, not the cold one.
	_ = start().Close()
	time.Sleep(500 * time.Millisecond)
	base := runtime.NumGoroutine()

	// The caller's context is never cancelled — exactly the bridge's shape.
	for i := 0; i < 3; i++ {
		_ = start().Close()
	}
	// A few stragglers from libp2p's own teardown are tolerated; a leaked
	// Service is dozens of goroutines.
	const slack = 5
	if n := settle(base + slack); n > base+slack {
		buf := make([]byte, 1<<20)
		dump := string(buf[:runtime.Stack(buf, true)])
		var ours []string
		for _, g := range strings.Split(dump, "\n\n") {
			if strings.Contains(g, "concord/internal/") {
				ours = append(ours, g)
			}
		}
		t.Fatalf("goroutines after three Start+Close: %d, baseline %d\n%s", n, base, strings.Join(ours, "\n\n"))
	}
}
