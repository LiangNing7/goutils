package distlock

import (
	"context"
	"sync"
	"time"
)

// renewFunc is called on every renewal tick. It receives a context that is
// derived from the acquisition context (detached from the caller's own
// cancellation) and is cancelled when the acquisition ends.
//
// A renewFunc MUST NOT lock the locker's operation mutex: lockerState.release
// joins the goroutine while holding it, so taking it from the goroutine would
// deadlock.
type renewFunc func(ctx context.Context)

// lockerState is embedded by every backend. It holds the per-instance ownership
// bookkeeping (which token this instance currently owns) and the renewal
// goroutine for the current acquisition.
//
// Lock ordering: opMu -> mu -> renewManager.mu. Nothing acquires them in
// reverse, and no lock is held while calling into the backend.
type lockerState struct {
	opMu sync.Mutex // serialises the public Lock/Unlock/Renew calls

	mu    sync.Mutex // guards held and token
	held  bool
	token string

	renew renewManager
}

// lockOp serialises the public operations of a single locker instance. The
// returned function releases the lock and is safe to use with defer.
func (s *lockerState) lockOp() func() {
	s.opMu.Lock()
	return s.opMu.Unlock
}

// isHeld reports whether this instance currently owns the lock, according to
// its own bookkeeping. It is a local check used to reject a re-entrant Lock; it
// is never used to decide ownership in the backend.
func (s *lockerState) isHeld() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held
}

// heldToken returns the token of the current acquisition and whether the
// instance believes it holds the lock.
func (s *lockerState) heldToken() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token, s.held
}

// markHeld records a successful acquisition of token and, when renew is not nil,
// starts the background renewal goroutine.
//
// renew == nil means "the backend renews automatically" (etcd keeps the lease
// alive from its session, ZooKeeper holds an ephemeral node for as long as the
// session lives).
func (s *lockerState) markHeld(
	token string,
	parent context.Context,
	interval time.Duration,
	renew renewFunc,
) {
	s.mu.Lock()
	s.token, s.held = token, true
	s.mu.Unlock()

	if renew != nil {
		s.renew.start(parent, interval, renew)
	}
}

// clearHeld stops the renewal goroutine and forgets the current token. It
// returns the token that was held and whether the instance actually held the
// lock, so callers can implement idempotent Unlock.
//
// It is safe to call concurrently and repeatedly.
func (s *lockerState) clearHeld() (string, bool) {
	// Stop (and join) the goroutine before touching the token: the goroutine
	// reads the token it captured, never this field, but joining first keeps
	// the ordering obvious.
	s.renew.stop()

	s.mu.Lock()
	defer s.mu.Unlock()
	token, held := s.token, s.held
	s.token, s.held = "", false
	return token, held
}

// renewManager owns the renewal goroutine of a single acquisition.
//
// It replaces the previous "shared *time.Ticker field + never-closed stop
// channel" scheme, which caused:
//
//   - a data race: Unlock wrote the shared ticker field while the goroutine read
//     it; because select evaluates every channel operand the goroutine could
//     dereference a nil ticker and crash the process with SIGSEGV;
//   - a goroutine leak: nothing ever closed stopChan, and Ticker.Stop does not
//     close the ticker channel, so the goroutine stayed blocked forever.
//
// The ticker is now a local variable of the goroutine and termination goes
// through a context, so both defects are structurally impossible.
type renewManager struct {
	mu     sync.Mutex
	ticker *time.Ticker
	cancel context.CancelFunc
	done   chan struct{}
}

// start launches the renewal goroutine. It is a no-op when a goroutine is
// already running (a second Lock always fails before reaching this point, so
// this is belt and braces).
func (m *renewManager) start(parent context.Context, interval time.Duration, renew renewFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done != nil {
		return
	}

	// Detach from the caller's cancellation. If we inherited the caller's
	// context, the idiomatic `ctx, cancel := context.WithTimeout(...); defer
	// cancel()` around a Lock call would silently stop the renewal and let the
	// lock expire while its holder still thinks it owns it.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))

	// Local variables only: the goroutine must never read a field that Unlock
	// can overwrite.
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	m.ticker, m.cancel, m.done = ticker, cancel, done

	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				renew(ctx)
			}
		}
	}()
}

// stop cancels the renewal goroutine and waits for it to return. It is
// idempotent: the state is cleared under the mutex, so a second caller finds
// done == nil and returns immediately instead of double-closing a channel.
func (m *renewManager) stop() {
	m.mu.Lock()
	ticker, cancel, done := m.ticker, m.cancel, m.done
	m.ticker, m.cancel, m.done = nil, nil, nil
	m.mu.Unlock()

	if done == nil {
		return
	}

	cancel()
	if ticker != nil {
		ticker.Stop()
	}
	<-done
}
