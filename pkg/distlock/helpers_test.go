package distlock

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/LiangNing7/goutils/pkg/logger"
	"github.com/LiangNing7/goutils/pkg/logger/empty"
)

// quietLogger returns a logger that discards everything, used by the tests that
// would otherwise print thousands of lines.
func quietLogger() logger.Logger { return empty.NewLogger() }

// isErrLockNotHeld and isErrLockAlreadyHeld are the two assertions every backend
// test needs; the sentinels are always wrapped with %w, so errors.Is is the
// contract being checked.
func isErrLockNotHeld(err error) bool { return errors.Is(err, ErrLockNotHeld) }

func isErrLockAlreadyHeld(err error) bool { return errors.Is(err, ErrLockAlreadyHeld) }

// countingLogger is a logger.Logger that counts how many times each message was
// logged. It lets tests observe the renewal goroutine without adding test hooks
// to the production code.
type countingLogger struct {
	mu       sync.Mutex
	counters map[string]int
}

func (c *countingLogger) record(message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counters == nil {
		c.counters = make(map[string]int)
	}
	c.counters[message]++
}

func (c *countingLogger) count(message string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counters[message]
}

func (c *countingLogger) Debug(message string, keysAndValues ...any) { c.record(message) }
func (c *countingLogger) Info(message string, keysAndValues ...any)  { c.record(message) }
func (c *countingLogger) Warn(message string, keysAndValues ...any)  { c.record(message) }
func (c *countingLogger) Error(message string, keysAndValues ...any) { c.record(message) }

// waitForGoroutines waits for the goroutine count to fall back to the given
// baseline, failing the test after a generous timeout. Polling keeps the test
// deterministic instead of timing sensitive.
func waitForGoroutines(t *testing.T, baseline int, iterations int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= baseline {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines leaked: baseline=%d after %d iterations=%d", baseline, iterations, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
