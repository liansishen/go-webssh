package ws

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/liansishen/go-webssh/internal/config"
)

func TestReserveSessionHonorsConcurrentLimit(t *testing.T) {
	h := &Handler{Cfg: &config.Config{SSH: config.SSHConfig{MaxSessions: 3}}}
	var successes atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h.reserveSession() {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 3 || h.activeSessions.Load() != 3 {
		t.Fatalf("successes=%d active=%d", successes.Load(), h.activeSessions.Load())
	}
}
