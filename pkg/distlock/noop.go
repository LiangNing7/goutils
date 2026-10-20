package distlock

import (
	"context"
	"fmt"
	"time"

	"github.com/LiangNing7/goutils/pkg/logger"
)

// NoopLocker provides a no-operation implementation of a distributed lock.
//
// It does not talk to any backend, but it is *not* a no-op any more: it models
// the full local lifecycle (token minting, mutual exclusion between successive
// Lock calls on the same instance, renewal bookkeeping). It is the reference
// implementation used by the package test suite. It still provides no mutual
// exclusion whatsoever across processes — never use it in production.
type NoopLocker struct {
	lockName    string
	lockTimeout time.Duration
	ownerID     string
	logger      logger.Logger

	st lockerState
}

// Ensure NoopLocker implements the Locker and Closer interfaces.
var (
	_ Locker = (*NoopLocker)(nil)
	_ Closer = (*NoopLocker)(nil)
)

// NewNoopLocker creates a new NoopLocker instance.
//
// A lock timeout below MinLockTimeout is clamped to MinLockTimeout (with a
// warning) because this constructor cannot report an error.
func NewNoopLocker(opts ...Option) *NoopLocker {
	o := ApplyOptions(opts...)
	return &NoopLocker{
		lockName:    o.lockName,
		lockTimeout: o.clampTimeout(),
		ownerID:     o.ownerID,
		logger:      o.logger,
	}
}

// Lock simulates acquiring a distributed lock.
func (l *NoopLocker) Lock(ctx context.Context) error {
	defer l.st.lockOp()()

	if l.st.isHeld() {
		return fmt.Errorf("%w (noop name=%s)", ErrLockAlreadyHeld, l.lockName)
	}

	token := newToken()
	l.st.markHeld(token, ctx, l.lockTimeout/2, func(rctx context.Context) {
		if err := l.renewLocked(); err != nil {
			l.logger.Error("failed to renew lock", "name", l.lockName, "error", err)
		}
	})

	l.logger.Info("Lock acquired", "name", l.lockName, "ownerID", l.ownerID, "token", token)
	return nil
}

// Unlock simulates releasing a distributed lock. It is idempotent: releasing a
// lock this instance does not hold returns ErrLockNotHeld and does nothing else.
func (l *NoopLocker) Unlock(ctx context.Context) error {
	defer l.st.lockOp()()

	if _, held := l.st.clearHeld(); !held {
		return fmt.Errorf("%w (noop name=%s)", ErrLockNotHeld, l.lockName)
	}

	l.logger.Info("Lock released", "name", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Renew reports whether this instance still holds the lock. There is nothing to
// extend for the no-op backend, but the code path is shared with the renewal
// goroutine so tests can observe that renewal really happens.
func (l *NoopLocker) Renew(ctx context.Context) error {
	defer l.st.lockOp()()
	return l.renewLocked()
}

// renewLocked is the renewal code path used by both Renew and the background
// goroutine. It deliberately does not take the operation lock, so that
// lockerState.clearHeld can join the goroutine while holding it.
func (l *NoopLocker) renewLocked() error {
	if !l.st.isHeld() {
		return fmt.Errorf("%w (noop name=%s)", ErrLockNotHeld, l.lockName)
	}
	l.logger.Info("Lock renewed", "name", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Close stops the renewal goroutine. It is idempotent.
func (l *NoopLocker) Close() error {
	defer l.st.lockOp()()
	l.st.clearHeld()
	return nil
}
