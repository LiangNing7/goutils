package distlock

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/LiangNing7/goutils/pkg/logger"
)

// consulKV is the subset of *api.KV used by ConsulLocker.
type consulKV interface {
	Acquire(p *api.KVPair, q *api.WriteOptions) (bool, *api.WriteMeta, error)
	Release(p *api.KVPair, q *api.WriteOptions) (bool, *api.WriteMeta, error)
}

// consulSession is the subset of *api.Session used by ConsulLocker.
type consulSession interface {
	Create(se *api.SessionEntry, q *api.WriteOptions) (string, *api.WriteMeta, error)
	Renew(id string, q *api.WriteOptions) (*api.SessionEntry, *api.WriteMeta, error)
	Destroy(id string, q *api.WriteOptions) (*api.WriteMeta, error)
}

// ConsulLocker provides a distributed locking mechanism using Consul.
//
// Design:
//
//   - Each acquisition creates a Consul session whose lifetime is the lease,
//     and acquires the key with KV().Acquire(pair) where pair.Session is that
//     session id. Acquire returns a bool that MUST be checked: Consul answers
//     "false, no error" when the key is already held, and the previous
//     implementation discarded that bool, so every Lock "succeeded".
//   - The session id is kept on the locker and used by Renew. The previous
//     implementation passed the hostname to Session().Renew, which could only
//     ever fail, and with Behavior=release the lock was silently dropped.
//   - Unlock releases the key with KV().Release and then destroys the session.
//   - Close destroys the session (which releases the key) and stops the renewal
//     goroutine.
type ConsulLocker struct {
	kv          consulKV
	sessions    consulSession
	lockKey     string
	lockTimeout time.Duration
	ownerID     string
	logger      logger.Logger

	st lockerState

	// sessionID belongs to the current acquisition. It is only touched while
	// holding the operation lock (and captured by the renewal goroutine).
	sessionID string
}

// Ensure ConsulLocker implements the Locker and Closer interfaces.
var (
	_ Locker = (*ConsulLocker)(nil)
	_ Closer = (*ConsulLocker)(nil)
)

// Consul session TTL bounds. Consul's server rejects a session whose TTL is
// outside this range (Session HTTP API: "between 10s and 86400s"), so a lock
// timeout outside it would fail at Lock time; the constructor rejects it up
// front instead of leaving a runtime failure for the caller to discover.
const (
	consulMinSessionTTL = 10 * time.Second
	consulMaxSessionTTL = 86400 * time.Second
)

// NewConsulLocker creates a new ConsulLocker instance.
//
// A lock timeout below MinLockTimeout is rejected with ErrInvalidLockTimeout, and
// so is one outside Consul's session TTL range (consulMinSessionTTL ..
// consulMaxSessionTTL) — note that this makes sub-10s lock timeouts impossible on
// Consul.
func NewConsulLocker(consulAddr string, opts ...Option) (*ConsulLocker, error) {
	o := ApplyOptions(opts...)
	if err := o.validateErr(); err != nil {
		return nil, err
	}
	if o.lockTimeout < consulMinSessionTTL || o.lockTimeout > consulMaxSessionTTL {
		return nil, fmt.Errorf("%w: consul session TTL must be between %s and %s, got %s",
			ErrInvalidLockTimeout, consulMinSessionTTL, consulMaxSessionTTL, o.lockTimeout)
	}

	config := api.DefaultConfig()
	config.Address = consulAddr
	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}

	locker := newConsulLocker(client.KV(), client.Session(), o)
	return locker, nil
}

// newConsulLocker wires a ConsulLocker around already resolved KV and Session
// handles. It exists so tests can inject fakes.
func newConsulLocker(kv consulKV, sessions consulSession, o *Options) *ConsulLocker {
	locker := &ConsulLocker{
		kv:          kv,
		sessions:    sessions,
		lockKey:     o.lockName,
		lockTimeout: o.lockTimeout,
		ownerID:     o.ownerID,
		logger:      o.logger,
	}

	locker.logger.Info("ConsulLocker initialized", "lockKey", locker.lockKey, "ownerID", locker.ownerID)
	return locker
}

// consulTTL renders the lock timeout the way Consul expects a session TTL:
// a whole number of seconds, rounded up.
//
// Note that the previous implementation used fmt.Sprintf("%s", l.lockTimeout),
// which happens to produce "10s" because time.Duration implements Stringer. That
// was not a bug; this helper simply makes the rounding explicit.
func consulTTL(timeout time.Duration) string {
	seconds := int64(math.Ceil(timeout.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%ds", seconds)
}

// Lock attempts to acquire the distributed lock.
func (l *ConsulLocker) Lock(ctx context.Context) error {
	defer l.st.lockOp()()

	if l.st.isHeld() {
		return fmt.Errorf("%w (consul key=%s)", ErrLockAlreadyHeld, l.lockKey)
	}

	token := newToken()

	sessionID, _, err := l.sessions.Create(&api.SessionEntry{
		TTL:      consulTTL(l.lockTimeout),
		Behavior: api.SessionBehaviorRelease,
	}, nil)
	if err != nil {
		l.logger.Error("failed to create session", "key", l.lockKey, "error", err)
		return fmt.Errorf("distlock/consul: create session: %w", err)
	}

	pair := &api.KVPair{
		Key:     l.lockKey,
		Value:   []byte(token),
		Session: sessionID,
	}

	acquired, _, err := l.kv.Acquire(pair, nil)
	if err != nil {
		l.destroySession(sessionID)
		l.logger.Error("failed to acquire lock", "key", l.lockKey, "error", err)
		return fmt.Errorf("distlock/consul: acquire: %w", err)
	}
	if !acquired {
		// The bool is the verdict. Consul returns (false, nil, nil) when the key
		// is already held by another session.
		l.destroySession(sessionID)
		l.logger.Warn("lock is already held", "key", l.lockKey)
		return fmt.Errorf("%w (consul key=%s)", ErrLockAlreadyHeld, l.lockKey)
	}

	l.sessionID = sessionID

	l.st.markHeld(token, ctx, l.lockTimeout/2, func(rctx context.Context) {
		if err := l.renewSession(rctx, sessionID); err != nil {
			l.logger.Error("failed to renew lock", "key", l.lockKey, "error", err)
		}
	})

	l.logger.Info("Lock acquired", "key", l.lockKey, "ownerID", l.ownerID, "sessionID", sessionID)
	return nil
}

// Unlock releases the key and destroys the session that backed it.
func (l *ConsulLocker) Unlock(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.clearHeld()
	sessionID := l.sessionID
	l.sessionID = ""
	if !held {
		if sessionID != "" {
			l.destroySession(sessionID)
		}
		return fmt.Errorf("%w (consul key=%s)", ErrLockNotHeld, l.lockKey)
	}

	// Release only succeeds while the session still holds the key. If our
	// session expired and somebody else took over, Release is a no-op and their
	// lock survives — exactly what we want.
	released, _, err := l.kv.Release(&api.KVPair{
		Key:     l.lockKey,
		Value:   []byte(token),
		Session: sessionID,
	}, nil)
	if err != nil {
		l.logger.Error("failed to release lock", "key", l.lockKey, "error", err)
		l.destroySession(sessionID)
		return fmt.Errorf("distlock/consul: release: %w", err)
	}

	l.destroySession(sessionID)

	if !released {
		l.logger.Warn("lock is not ours any more, leaving it untouched", "key", l.lockKey)
		return fmt.Errorf("%w (consul key=%s: session expired or taken over)", ErrLockNotHeld, l.lockKey)
	}

	l.logger.Info("Lock released", "key", l.lockKey, "ownerID", l.ownerID)
	return nil
}

// Renew refreshes the lock's expiration time by renewing the session that backs
// it. The session id used here is the one stored by Lock — renewing by hostname,
// as the previous implementation did, can never work.
func (l *ConsulLocker) Renew(ctx context.Context) error {
	defer l.st.lockOp()()

	if _, held := l.st.heldToken(); !held {
		return fmt.Errorf("%w (consul key=%s)", ErrLockNotHeld, l.lockKey)
	}
	if l.sessionID == "" {
		return fmt.Errorf("%w (consul key=%s: no session)", ErrLockNotHeld, l.lockKey)
	}
	return l.renewSession(ctx, l.sessionID)
}

// renewSession renews the given session. It never takes the operation lock: it
// is called from Renew (which holds it) and from the renewal goroutine (which
// must not, see lockerState).
func (l *ConsulLocker) renewSession(ctx context.Context, sessionID string) error {
	if _, _, err := l.sessions.Renew(sessionID, nil); err != nil {
		return fmt.Errorf("%w (consul key=%s: session renew failed: %v)", ErrLockNotHeld, l.lockKey, err)
	}
	return nil
}

// destroySession destroys a session, ignoring failures (the session may already
// have expired, in which case Consul reports that it does not know it).
func (l *ConsulLocker) destroySession(sessionID string) {
	if sessionID == "" {
		return
	}
	if _, err := l.sessions.Destroy(sessionID, nil); err != nil {
		l.logger.Warn("failed to destroy session", "key", l.lockKey, "sessionID", sessionID, "error", err)
	}
}

// Close stops the renewal goroutine and destroys the session of the current
// acquisition, which also releases the key (Behavior=release).
func (l *ConsulLocker) Close() error {
	defer l.st.lockOp()()
	l.st.clearHeld()
	l.destroySession(l.sessionID)
	l.sessionID = ""
	return nil
}
