package distlock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/LiangNing7/goutils/pkg/logger"
)

// memcacheClient is the subset of gomemcache used by MemcachedLocker. It exists
// so the locker can be unit tested with a fake; *memcache.Client satisfies it.
type memcacheClient interface {
	Add(item *memcache.Item) error
	Get(key string) (*memcache.Item, error)
	Delete(key string) error
	CompareAndSwap(item *memcache.Item) error
}

// memcachedMaxExpiration is the largest relative expiration memcached accepts.
// Anything above 30 days is interpreted as an absolute unix timestamp, which
// would put the expiration somewhere in 1970 and make the lock effectively
// immortal.
const memcachedMaxExpiration = 30*24*time.Hour - time.Second

// memcachedExpiration converts a lock timeout into the whole-second expiration
// understood by memcached, rounding up.
//
// The rounding up is load bearing: `int32(timeout.Seconds())` truncates, and a
// timeout below one second truncates to 0 — and 0 means "never expire" for
// memcached, i.e. a permanent deadlock that survives every crash.
func memcachedExpiration(timeout time.Duration) int32 {
	if timeout < time.Second {
		timeout = time.Second
	}
	if timeout > memcachedMaxExpiration {
		timeout = memcachedMaxExpiration
	}
	return int32((timeout + time.Second - 1) / time.Second)
}

// MemcachedLocker provides a distributed locking mechanism using Memcached.
//
// KNOWN LIMITATION — Unlock is NOT atomic. Memcached has no compare-and-delete
// primitive, so Unlock performs GET-then-DELETE: if your lease expires between
// the two calls and another owner takes the lock in that window, the DELETE
// removes *their* lock. The window is one round trip wide and can only open when
// the lease is expiring anyway, but it is real and cannot be closed with a stock
// memcached protocol. Renew does not have this problem: it uses GET (which
// returns the CAS id) followed by CAS, which is a genuine atomic
// compare-and-swap.
//
// Because of the above, prefer Redis/etcd/Consul when correct mutual exclusion
// matters, and treat the memcached backend as best effort.
type MemcachedLocker struct {
	client      memcacheClient
	lockName    string
	lockTimeout time.Duration
	ownerID     string
	logger      logger.Logger

	st lockerState
}

// Ensure MemcachedLocker implements the Locker and Closer interfaces.
var (
	_ Locker = (*MemcachedLocker)(nil)
	_ Closer = (*MemcachedLocker)(nil)
)

// NewMemcachedLocker creates a new MemcachedLocker instance.
//
// A lock timeout below MinLockTimeout is clamped to MinLockTimeout (with a
// warning) because this constructor cannot report an error. The value is also
// capped at 30 days, see memcachedExpiration.
func NewMemcachedLocker(memcachedAddr string, opts ...Option) *MemcachedLocker {
	o := ApplyOptions(opts...)
	locker := &MemcachedLocker{
		client:      memcache.New(memcachedAddr),
		lockName:    o.lockName,
		lockTimeout: o.clampTimeout(),
		ownerID:     o.ownerID,
		logger:      o.logger,
	}

	locker.logger.Info("MemcachedLocker initialized", "lockKey", locker.lockName, "ownerID", locker.ownerID)
	return locker
}

// Lock attempts to acquire the distributed lock.
func (l *MemcachedLocker) Lock(ctx context.Context) error {
	defer l.st.lockOp()()

	if l.st.isHeld() {
		return fmt.Errorf("%w (memcached key=%s)", ErrLockAlreadyHeld, l.lockName)
	}

	token := newToken()
	// Add only stores the item when the key does not exist yet, which is the
	// memcached equivalent of SET NX.
	err := l.client.Add(&memcache.Item{
		Key:        l.lockName,
		Value:      []byte(token),
		Expiration: memcachedExpiration(l.lockTimeout),
	})
	if errors.Is(err, memcache.ErrNotStored) {
		l.logger.Warn("lock is already held", "key", l.lockName)
		return fmt.Errorf("%w (memcached key=%s)", ErrLockAlreadyHeld, l.lockName)
	}
	if err != nil {
		l.logger.Error("failed to acquire lock", "key", l.lockName, "error", err)
		return fmt.Errorf("distlock/memcached: add: %w", err)
	}

	l.st.markHeld(token, ctx, l.lockTimeout/2, func(rctx context.Context) {
		if err := l.renewWithToken(rctx, token); err != nil {
			l.logger.Error("failed to renew lock", "key", l.lockName, "error", err)
		}
	})

	l.logger.Info("Lock acquired", "key", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Unlock releases the lock, but only if the stored value is still our token.
//
// The check (GET) and the mutation (DELETE) are two round trips: see the type
// documentation for the resulting race window. Memcached offers no atomic
// compare-and-delete.
func (l *MemcachedLocker) Unlock(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.clearHeld()
	if !held {
		return fmt.Errorf("%w (memcached key=%s)", ErrLockNotHeld, l.lockName)
	}

	current, err := l.client.Get(l.lockName)
	if errors.Is(err, memcache.ErrCacheMiss) {
		// The item already expired (or was evicted). That is not a failure of
		// ours, but we no longer own anything.
		l.logger.Warn("lock already expired before it could be released", "key", l.lockName)
		return fmt.Errorf("%w (memcached key=%s: already expired)", ErrLockNotHeld, l.lockName)
	}
	if err != nil {
		l.logger.Error("failed to read lock before releasing", "key", l.lockName, "error", err)
		return fmt.Errorf("distlock/memcached: get: %w", err)
	}
	if string(current.Value) != token {
		l.logger.Warn("lock is not ours any more, leaving it untouched", "key", l.lockName)
		return fmt.Errorf("%w (memcached key=%s: taken over)", ErrLockNotHeld, l.lockName)
	}

	if err := l.client.Delete(l.lockName); err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		l.logger.Error("failed to release lock", "key", l.lockName, "error", err)
		return fmt.Errorf("distlock/memcached: delete: %w", err)
	}

	l.logger.Info("Lock released", "key", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Renew refreshes the lock's expiration time, but only if the stored value is
// still our token.
func (l *MemcachedLocker) Renew(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.heldToken()
	if !held {
		return fmt.Errorf("%w (memcached key=%s)", ErrLockNotHeld, l.lockName)
	}
	return l.renewWithToken(ctx, token)
}

// renewWithToken performs the compare-and-swap renewal. It never takes the
// operation lock: it is called from Renew (which holds it) and from the renewal
// goroutine (which must not, see lockerState).
//
// This one *is* atomic: Get issues the memcached "gets" command and records the
// CAS id, and CompareAndSwap only succeeds if the item is still the very same
// version. If somebody else replaced the lock, CAS fails with ErrCASConflict.
func (l *MemcachedLocker) renewWithToken(ctx context.Context, token string) error {
	current, err := l.client.Get(l.lockName)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return fmt.Errorf("%w (memcached key=%s: lease expired)", ErrLockNotHeld, l.lockName)
	}
	if err != nil {
		return fmt.Errorf("distlock/memcached: get: %w", err)
	}
	if string(current.Value) != token {
		return fmt.Errorf("%w (memcached key=%s: taken over)", ErrLockNotHeld, l.lockName)
	}

	err = l.client.CompareAndSwap(&memcache.Item{
		Key:        l.lockName,
		Value:      []byte(token),
		Expiration: memcachedExpiration(l.lockTimeout),
		CasID:      current.CasID,
	})
	if errors.Is(err, memcache.ErrCASConflict) || errors.Is(err, memcache.ErrNotStored) {
		return fmt.Errorf("%w (memcached key=%s: taken over)", ErrLockNotHeld, l.lockName)
	}
	if errors.Is(err, memcache.ErrCacheMiss) {
		return fmt.Errorf("%w (memcached key=%s: lease expired)", ErrLockNotHeld, l.lockName)
	}
	if err != nil {
		return fmt.Errorf("distlock/memcached: cas: %w", err)
	}
	return nil
}

// Close stops the renewal goroutine. gomemcache clients have no Close, so there
// is nothing else to release.
func (l *MemcachedLocker) Close() error {
	defer l.st.lockOp()()
	l.st.clearHeld()
	return nil
}
