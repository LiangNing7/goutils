package distlock

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/LiangNing7/goutils/pkg/logger"
)

// EtcdLocker provides a distributed locking mechanism using etcd.
//
// This backend is built on go.etcd.io/etcd/client/v3/concurrency, which is the
// canonical etcd locking primitive and ships in a module this package already
// depends on:
//
//   - A Session owns a lease whose TTL is the lock timeout; the client library
//     keeps that lease alive with a heartbeat, which is what makes the lock
//     survive for as long as the holder lives and disappear when it dies. There
//     is therefore no renewal goroutine and no ticker here.
//   - A Mutex is a lease-bound key under "<lockKey>/" whose ownership is decided
//     by create revision, so mutual exclusion is exact and fair. TryLock returns
//     concurrency.ErrLocked instead of blocking, which preserves this package's
//     non-blocking Lock contract.
//   - Unlock deletes the session-scoped key, a key no other session can own, so
//     it is impossible to release somebody else's lock by construction.
//
// The previous implementation used a bare Put, which unconditionally overwrote
// the key and returned nil, so every concurrent Lock "succeeded": there was no
// mutual exclusion at all.
//
// Trade-off (documented): because ownership is delegated to the concurrency
// package, this locker has no unit-testable seam and could not be exercised
// against a real cluster in this change — see the README "test coverage" table.
type EtcdLocker struct {
	cli         *clientv3.Client
	lockKey     string
	lockTimeout time.Duration
	ownerID     string
	logger      logger.Logger

	st lockerState

	session       *concurrency.Session
	mutex         *concurrency.Mutex
	sessionCancel context.CancelFunc
}

// Ensure EtcdLocker implements the Locker and Closer interfaces.
var (
	_ Locker = (*EtcdLocker)(nil)
	_ Closer = (*EtcdLocker)(nil)
)

// NewEtcdLocker initializes a new EtcdLocker instance.
//
// A lock timeout below MinLockTimeout is rejected with ErrInvalidLockTimeout
// (etcd leases are expressed in whole seconds).
func NewEtcdLocker(endpoints []string, opts ...Option) (*EtcdLocker, error) {
	o := ApplyOptions(opts...)
	if err := o.validateErr(); err != nil {
		return nil, err
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	locker := &EtcdLocker{
		cli:         cli,
		lockKey:     o.lockName,
		lockTimeout: o.lockTimeout,
		ownerID:     o.ownerID,
		logger:      o.logger,
	}

	return locker, nil
}

// etcdTTL converts the lock timeout into the whole-second lease TTL etcd wants,
// rounding up so that a timeout is never shortened.
func etcdTTL(timeout time.Duration) int {
	seconds := int(math.Ceil(timeout.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}

// Lock acquires the distributed lock.
func (l *EtcdLocker) Lock(ctx context.Context) error {
	defer l.st.lockOp()()

	if l.st.isHeld() {
		return fmt.Errorf("%w (etcd key=%s)", ErrLockAlreadyHeld, l.lockKey)
	}

	// Bound lease creation to this acquisition, while allowing the session
	// heartbeat to outlive the caller's context after Lock succeeds.
	lease, err := l.cli.Grant(ctx, int64(etcdTTL(l.lockTimeout)))
	if err != nil {
		return fmt.Errorf("distlock/etcd: grant lease: %w", err)
	}
	sessionCtx, cancelSession := context.WithCancel(l.cli.Ctx())
	stopCancel := context.AfterFunc(ctx, cancelSession)
	acquired := false
	defer func() {
		stopCancel()
		if !acquired {
			cancelSession()
		}
	}()
	session, err := concurrency.NewSession(l.cli,
		concurrency.WithTTL(etcdTTL(l.lockTimeout)),
		concurrency.WithLease(lease.ID), concurrency.WithContext(sessionCtx))
	if err != nil {
		return fmt.Errorf("distlock/etcd: create session: %w", err)
	}

	mutex := concurrency.NewMutex(session, l.lockKey)
	if err := mutex.TryLock(ctx); err != nil {
		cancelSession()
		_ = session.Close()
		if errors.Is(err, concurrency.ErrLocked) {
			l.logger.Warn("lock is already held", "key", l.lockKey)
			return fmt.Errorf("%w (etcd key=%s)", ErrLockAlreadyHeld, l.lockKey)
		}
		l.logger.Error("failed to acquire lock", "key", l.lockKey, "error", err)
		return fmt.Errorf("distlock/etcd: try lock: %w", err)
	}

	if !stopCancel() || ctx.Err() != nil {
		cancelSession()
		_ = session.Close()
		return ctx.Err()
	}
	acquired = true
	l.sessionCancel = cancelSession
	l.session, l.mutex = session, mutex

	// No renewal goroutine: the session heartbeat is the renewal. The mutex key
	// (unique per session) is recorded as the token purely for diagnostics.
	l.st.markHeld(mutex.Key(), ctx, 0, nil)

	l.logger.Info("Lock acquired", "key", l.lockKey, "ownerID", l.ownerID, "mutexKey", mutex.Key())
	return nil
}

// Unlock releases the distributed lock and revokes the lease that backed it.
func (l *EtcdLocker) Unlock(ctx context.Context) error {
	defer l.st.lockOp()()

	_, held := l.st.clearHeld()
	mutex, session := l.mutex, l.session
	if l.sessionCancel != nil {
		defer l.sessionCancel()
		l.sessionCancel = nil
	}
	l.mutex, l.session = nil, nil

	if !held || mutex == nil {
		if session != nil {
			_ = session.Close()
		}
		return fmt.Errorf("%w (etcd key=%s)", ErrLockNotHeld, l.lockKey)
	}

	unlockErr := mutex.Unlock(ctx)
	// Closing the session revokes the lease, so even if the explicit Unlock
	// failed the key disappears with the lease.
	if session != nil {
		if err := session.Close(); err != nil {
			l.logger.Warn("failed to close etcd session", "key", l.lockKey, "error", err)
		}
	}

	if unlockErr != nil && !errors.Is(unlockErr, concurrency.ErrLockReleased) {
		l.logger.Error("failed to release lock", "key", l.lockKey, "error", unlockErr)
		return fmt.Errorf("distlock/etcd: unlock: %w", unlockErr)
	}

	l.logger.Info("Lock released", "key", l.lockKey, "ownerID", l.ownerID)
	return nil
}

// Renew checks that the lease backing the lock is still alive.
//
// There is nothing to extend explicitly: go.etcd.io/etcd/client/v3/concurrency
// keeps the session lease alive with its own heartbeat, which is why this locker
// has no renewal goroutine. A dead session (which means the lock is gone) is
// reported as ErrLockNotHeld.
func (l *EtcdLocker) Renew(ctx context.Context) error {
	defer l.st.lockOp()()

	if _, held := l.st.heldToken(); !held || l.session == nil {
		return fmt.Errorf("%w (etcd key=%s)", ErrLockNotHeld, l.lockKey)
	}

	select {
	case <-l.session.Done():
		return fmt.Errorf("%w (etcd key=%s: session expired)", ErrLockNotHeld, l.lockKey)
	default:
		return nil
	}
}

// Close stops the renewal bookkeeping, revokes the session lease and closes the
// etcd client this locker created.
func (l *EtcdLocker) Close() error {
	defer l.st.lockOp()()
	l.st.clearHeld()

	session := l.session
	if l.sessionCancel != nil {
		defer l.sessionCancel()
		l.sessionCancel = nil
	}
	l.session, l.mutex = nil, nil
	if session != nil {
		if err := session.Close(); err != nil {
			l.logger.Warn("failed to close etcd session", "key", l.lockKey, "error", err)
		}
	}

	if err := l.cli.Close(); err != nil {
		return fmt.Errorf("distlock/etcd: close client: %w", err)
	}
	return nil
}
