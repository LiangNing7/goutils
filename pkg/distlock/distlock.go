// Package distlock provides distributed lock implementations backed by Redis,
// MongoDB, GORM (MySQL / PostgreSQL / SQLite), Consul, ZooKeeper, Memcached or
// etcd.
//
// # Correctness model
//
// Every successful Lock mints a fresh, high-entropy ownership token (a UUIDv4,
// see newToken) and stores it alongside the lock. Unlock and Renew are
// *conditional* operations: they only take effect when the value currently
// stored under the lock still equals the token minted by this very acquisition.
// A holder that lost its lock (its lease expired and somebody else acquired it)
// can therefore never delete or extend somebody else's lock — the classic
// "A's TTL expires, B acquires, A calls Unlock and destroys B's lock" bug.
//
// ownerID is informational only: it appears in log lines to help operators, and
// it never takes part in the ownership decision. Two processes on the same host
// (same hostname) can therefore no longer confuse each other.
//
// # Renewal
//
// Each acquisition starts a background goroutine that periodically calls Renew.
// Two properties matter:
//
//   - The goroutine runs on a context derived with context.WithoutCancel(ctx),
//     so cancelling the context you passed to Lock (the ubiquitous
//     `defer cancel()` pattern) does not silently stop the renewal and let the
//     lock expire under the holder's feet.
//   - The goroutine captures its *time.Ticker* as a local variable and is
//     stopped+joined by Unlock/Close through a per-acquisition channel. It can
//     neither race with Unlock nor leak.
//
// # Errors
//
// Unlock and Renew return ErrLockNotHeld when this locker does not (or no
// longer) own the lock, and Lock returns ErrLockAlreadyHeld when the lock is
// held by somebody else. Both are meant to be inspected with errors.Is.
package distlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/LiangNing7/goutils/pkg/logger"
	"github.com/LiangNing7/goutils/pkg/logger/empty"
)

// DefaultLockName is the default name used for the distributed lock.
const DefaultLockName = "onex-distributed-lock"

// MinLockTimeout is the smallest lock timeout accepted by the constructors.
// It exists because several backends have second granularity (Memcached stores
// its expiration in whole seconds, etcd leases are expressed in seconds) and
// because the renewal interval is lockTimeout/2, which must stay positive.
const MinLockTimeout = time.Second

// Sentinel errors. All of them are wrapped with %w so callers can match them
// with errors.Is while still getting useful context in the message.
var (
	// ErrLockAlreadyHeld is returned by Lock when the lock is currently held
	// by another owner (or by this locker instance itself).
	ErrLockAlreadyHeld = errors.New("distlock: lock is already held by another owner")

	// ErrLockNotHeld is returned by Unlock and Renew when this locker does not
	// hold the lock: either it never acquired it, it already released it, or
	// the lease expired and somebody else took over. In the last case the lock
	// is deliberately left untouched.
	ErrLockNotHeld = errors.New("distlock: lock is not held by this locker")

	// ErrInvalidLockTimeout is returned by constructors that report errors when
	// the configured lock timeout is below MinLockTimeout.
	ErrInvalidLockTimeout = errors.New("distlock: lock timeout is too small")
)

// Locker is an interface that defines the methods for a distributed lock.
// It provides methods to acquire, release, and renew a lock in a distributed
// system.
//
// This interface is intentionally unchanged from earlier releases: the
// token/ownership rework was done behind it. Optional capabilities (Close) live
// on the concrete types and on the Closer interface below.
type Locker interface {
	// Lock attempts to acquire the lock.
	Lock(ctx context.Context) error

	// Unlock releases the previously acquired lock.
	Unlock(ctx context.Context) error

	// Renew updates the expiration time of the lock.
	// It should be called periodically to keep the lock active.
	Renew(ctx context.Context) error
}

// Closer is implemented by every concrete locker in this package. It stops the
// background renewal and releases the resources the locker owns: client
// connections and sessions for the backends that dial themselves (MongoDB,
// etcd, ZooKeeper, Consul), and just the renewal goroutine for the backends
// whose client is supplied by the caller (Redis, GORM, Memcached, Noop).
//
// Because a Consul session, an etcd lease and a ZooKeeper session are what
// actually holds the lock, Close on those backends also releases the lock. On
// Redis/GORM/Memcached/Noop it does not: call Unlock first, or let the lease
// expire.
//
// It is deliberately NOT part of Locker so that pre-existing implementations of
// Locker (for example test doubles owned by callers) keep compiling.
//
// Close is idempotent.
type Closer interface {
	Close() error
}

// Options holds the configuration for the distributed lock.
type Options struct {
	lockName    string        // Name of the lock
	lockTimeout time.Duration // Duration before the lock expires
	ownerID     string        // Identifier for the lock owner (informational)
	logger      logger.Logger // Logger for logging events
}

// Option is a function that modifies Options.
type Option func(o *Options)

// NewOptions initializes Options with default values.
func NewOptions() *Options {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	// The pid is appended so that two processes on the same host are still
	// distinguishable in logs. ownerID is diagnostic only — ownership is
	// decided by the per-acquisition token, never by ownerID.
	ownerID := fmt.Sprintf("%s-%d", hostname, os.Getpid())

	return &Options{
		lockName:    DefaultLockName,
		lockTimeout: 10 * time.Second,  // Default lock timeout
		ownerID:     ownerID,           // Set the owner ID
		logger:      empty.NewLogger(), // Default logger
	}
}

// ApplyOptions applies a series of Option functions to configure Options.
func ApplyOptions(opts ...Option) *Options {
	o := NewOptions() // Create a new Options instance with default values
	for _, opt := range opts {
		opt(o) // Apply each option to the Options instance
	}
	if o.logger == nil {
		// WithLogger(nil) would otherwise turn every log call into a nil
		// dereference panic inside a locker.
		o.logger = empty.NewLogger()
	}

	return o // Return the configured Options
}

// tickInterval is how often the background goroutine renews the lock. Half the
// lock timeout leaves room for one lost renewal round trip.
func (o *Options) tickInterval() time.Duration {
	return o.lockTimeout / 2
}

// validateErr returns ErrInvalidLockTimeout when the configured timeout is too
// small. Used by the constructors that already return an error.
func (o *Options) validateErr() error {
	if o.lockTimeout < MinLockTimeout {
		return fmt.Errorf("%w: got %s, minimum is %s", ErrInvalidLockTimeout, o.lockTimeout, MinLockTimeout)
	}
	return nil
}

// clampTimeout returns the configured timeout, raised to MinLockTimeout if it is
// too small, logging a warning. Used by constructors that cannot return an
// error (Redis, Memcached, Noop) so that an invalid value can never reach
// time.NewTicker (which panics) or produce a zero Memcached expiration (which
// means "never expire", i.e. a permanent deadlock).
func (o *Options) clampTimeout() time.Duration {
	if o.lockTimeout >= MinLockTimeout {
		return o.lockTimeout
	}
	o.logger.Warn("lock timeout is below the minimum, clamping",
		"requested", o.lockTimeout, "minimum", MinLockTimeout)
	return MinLockTimeout
}

// WithLockName sets the lock name in Options.
func WithLockName(name string) Option {
	return func(o *Options) {
		o.lockName = name // Set the lock name
	}
}

// WithLockTimeout sets the lock timeout in Options.
func WithLockTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.lockTimeout = timeout // Set the lock timeout
	}
}

// WithOwnerID sets the owner ID in Options.
func WithOwnerID(ownerID string) Option {
	return func(o *Options) {
		o.ownerID = ownerID // Set the owner ID
	}
}

// WithLogger sets the logger in Options.
func WithLogger(logger logger.Logger) Option {
	return func(o *Options) {
		o.logger = logger // Set the logger
	}
}

// newToken mints the ownership token for a single acquisition.
//
// It must be unguessable and collision free: the entire mutual-exclusion
// guarantee rests on "the value stored in the lock equals my token". A UUIDv4
// carries 122 bits of crypto/rand entropy, which is ample.
func newToken() string {
	return uuid.NewString()
}
