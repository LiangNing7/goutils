package distlock

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/LiangNing7/goutils/pkg/logger"
)

// redisClient is the subset of go-redis used by RedisLocker. It exists so the
// locker can be unit tested with a fake; *redis.Client satisfies it.
type redisClient interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd
}

// redisUnlockScript deletes the lock only when it still carries our token.
//
// This is the whole point of the token: a plain DEL would happily delete a lock
// that somebody else acquired after our lease expired.
//
// KEYS[1] = lock key, ARGV[1] = token. Returns 1 when deleted, 0 otherwise.
const redisUnlockScript = `
if redis.call('get', KEYS[1]) == ARGV[1] then
  return redis.call('del', KEYS[1])
end
return 0`

// redisRenewScript extends the TTL only when the lock still carries our token.
// PEXPIRE (milliseconds) is used instead of EXPIRE so sub-second timeouts keep
// their precision.
//
// KEYS[1] = lock key, ARGV[1] = token, ARGV[2] = ttl in milliseconds.
// Returns 1 when extended, 0 otherwise.
const redisRenewScript = `
if redis.call('get', KEYS[1]) == ARGV[1] then
  return redis.call('pexpire', KEYS[1], ARGV[2])
end
return 0`

// RedisLocker provides a distributed locking mechanism using Redis.
//
// Guarantees, and their limits:
//   - Mutual exclusion relies on SET NX and on the compare-and-delete/compare-
//     and-expire Lua scripts above; both are atomic on a single Redis instance.
//   - On a Redis Cluster the key lives in a single hash slot, so the scripts are
//     still atomic; failover can still lose the lock (Redis replication is
//     asynchronous). This is the usual Redlock caveat.
//   - The client is supplied by the caller; RedisLocker never closes it, so one
//     client can be shared by many lockers.
type RedisLocker struct {
	client      redisClient
	lockName    string
	lockTimeout time.Duration
	ownerID     string
	logger      logger.Logger

	st lockerState
}

// Ensure RedisLocker implements the Locker and Closer interfaces.
var (
	_ Locker = (*RedisLocker)(nil)
	_ Closer = (*RedisLocker)(nil)
)

// NewRedisLocker creates a new RedisLocker instance.
//
// A lock timeout below MinLockTimeout is clamped to MinLockTimeout (with a
// warning) because this constructor cannot report an error.
func NewRedisLocker(client redisClient, opts ...Option) *RedisLocker {
	o := ApplyOptions(opts...)
	locker := &RedisLocker{
		client:      client,
		lockName:    o.lockName,
		lockTimeout: o.clampTimeout(),
		ownerID:     o.ownerID,
		logger:      o.logger,
	}

	locker.logger.Info("RedisLocker initialized", "lockName", locker.lockName, "ownerID", locker.ownerID)
	return locker
}

// Lock attempts to acquire the distributed lock.
func (l *RedisLocker) Lock(ctx context.Context) error {
	defer l.st.lockOp()()

	if l.st.isHeld() {
		return fmt.Errorf("%w (redis key=%s)", ErrLockAlreadyHeld, l.lockName)
	}

	token := newToken()
	ok, err := l.client.SetNX(ctx, l.lockName, token, l.lockTimeout).Result()
	if err != nil {
		l.logger.Error("failed to set lock", "key", l.lockName, "error", err)
		return fmt.Errorf("distlock/redis: setnx: %w", err)
	}
	if !ok {
		// The key exists. Report who holds it for diagnostics, but never treat a
		// foreign value as our own acquisition: the value read back can only be
		// considered "ours" if it equals the token we just minted, which is
		// impossible for a fresh token.
		holder, getErr := l.client.Get(ctx, l.lockName).Result()
		if getErr != nil && !errors.Is(getErr, redis.Nil) {
			return fmt.Errorf("distlock/redis: get: %w", getErr)
		}
		l.logger.Warn("lock is already held", "key", l.lockName, "holder", holder)
		return fmt.Errorf("%w (redis key=%s holder=%q)", ErrLockAlreadyHeld, l.lockName, holder)
	}

	l.st.markHeld(token, ctx, l.lockTimeout/2, func(rctx context.Context) {
		if err := l.conditionalRenew(rctx, token); err != nil {
			l.logger.Error("failed to renew lock", "key", l.lockName, "error", err)
		}
	})

	l.logger.Info("Lock acquired", "key", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Unlock releases the distributed lock, but only if Redis still holds our token.
//
// When the lease expired and another owner took over, the lock is left alone and
// ErrLockNotHeld is returned. Unlock is idempotent.
func (l *RedisLocker) Unlock(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.clearHeld()
	if !held {
		return fmt.Errorf("%w (redis key=%s)", ErrLockNotHeld, l.lockName)
	}

	released, err := l.client.Eval(ctx, redisUnlockScript, []string{l.lockName}, token).Int64()
	if err != nil {
		l.logger.Error("failed to release lock", "key", l.lockName, "error", err)
		return fmt.Errorf("distlock/redis: eval unlock: %w", err)
	}
	if released == 0 {
		l.logger.Warn("lock is not ours any more, leaving it untouched", "key", l.lockName)
		return fmt.Errorf("%w (redis key=%s: lease expired or taken over)", ErrLockNotHeld, l.lockName)
	}

	l.logger.Info("Lock released", "key", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Renew refreshes the lock's expiration time, but only if Redis still holds our
// token.
func (l *RedisLocker) Renew(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.heldToken()
	if !held {
		return fmt.Errorf("%w (redis key=%s)", ErrLockNotHeld, l.lockName)
	}
	return l.conditionalRenew(ctx, token)
}

// conditionalRenew extends the lease only when the stored value is still our
// token. It never takes the operation lock: it is called both from Renew (which
// holds it) and from the renewal goroutine (which must not, see lockerState).
func (l *RedisLocker) conditionalRenew(ctx context.Context, token string) error {
	ttlMillis := strconv.FormatInt(l.lockTimeout.Milliseconds(), 10)
	extended, err := l.client.Eval(ctx, redisRenewScript, []string{l.lockName}, token, ttlMillis).Int64()
	if err != nil {
		return fmt.Errorf("distlock/redis: eval renew: %w", err)
	}
	if extended == 0 {
		return fmt.Errorf("%w (redis key=%s: lease expired or taken over)", ErrLockNotHeld, l.lockName)
	}
	return nil
}

// Close stops the renewal goroutine. It does not close the Redis client, which
// the caller owns and may share.
func (l *RedisLocker) Close() error {
	defer l.st.lockOp()()
	l.st.clearHeld()
	return nil
}
