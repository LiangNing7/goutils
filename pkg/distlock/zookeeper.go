package distlock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"

	"github.com/LiangNing7/goutils/pkg/logger"
)

// zkConn is the subset of *zk.Conn used by ZookeeperLocker.
type zkConn interface {
	Create(path string, data []byte, flags int32, acl []zk.ACL) (string, error)
	Get(path string) ([]byte, *zk.Stat, error)
	Delete(path string, version int32) error
	Close()
}

// ZookeeperLocker provides a distributed locking mechanism using ZooKeeper.
//
// Design:
//
//   - The lock is one EPHEMERAL znode at a path shared by every competitor
//     (the lock name). The previous implementation created a PERSISTENT node
//     named "<lockPath>/<ownerID>" instead: the path differed per owner, so two
//     processes never collided (zero mutual exclusion), and a crash left the
//     node behind forever.
//   - ErrNodeExists from Create means somebody holds the lock.
//   - The node data is the ownership token. Unlock re-reads the node and only
//     deletes it if the data is still our token, using the version CAS of
//     Delete, so we can never remove a node that was recreated by somebody else.
//   - ZooKeeper ephemeral nodes disappear when the session ends, so there is
//     nothing to renew: Renew only verifies that we still own the node and says
//     so honestly instead of logging a fake "Lock renewed".
//   - The path is normalised to start with "/" (ZooKeeper rejects relative
//     paths, which is what the default lock name used to produce).
type ZookeeperLocker struct {
	conn        zkConn
	lockPath    string
	lockTimeout time.Duration
	ownerID     string
	logger      logger.Logger

	st lockerState
}

// Ensure ZookeeperLocker implements the Locker and Closer interfaces.
var (
	_ Locker = (*ZookeeperLocker)(nil)
	_ Closer = (*ZookeeperLocker)(nil)
)

// normalizeZKPath makes sure the path is absolute, as ZooKeeper requires, and
// has no trailing slash.
func normalizeZKPath(path string) string {
	if path == "" {
		return "/" + DefaultLockName
	}
	path = "/" + strings.Trim(path, "/")
	if path == "/" {
		return "/" + DefaultLockName
	}
	return path
}

// NewZookeeperLocker creates a new ZookeeperLocker instance.
//
// A lock timeout below MinLockTimeout is rejected with ErrInvalidLockTimeout.
// Note that the timeout is only used for the local renewal bookkeeping: an
// ephemeral znode lives exactly as long as its ZooKeeper session.
func NewZookeeperLocker(zkServers []string, opts ...Option) (*ZookeeperLocker, error) {
	o := ApplyOptions(opts...)
	if err := o.validateErr(); err != nil {
		return nil, err
	}

	conn, _, err := zk.Connect(zkServers, 10*time.Second)
	if err != nil {
		return nil, err
	}

	locker := newZookeeperLocker(conn, o)
	return locker, nil
}

// newZookeeperLocker wires a ZookeeperLocker around an existing connection. It
// exists so tests can inject a fake connection.
func newZookeeperLocker(conn zkConn, o *Options) *ZookeeperLocker {
	locker := &ZookeeperLocker{
		conn:        conn,
		lockPath:    normalizeZKPath(o.lockName),
		lockTimeout: o.lockTimeout,
		ownerID:     o.ownerID,
		logger:      o.logger,
	}

	locker.logger.Info("ZookeeperLocker initialized", "lockPath", locker.lockPath, "ownerID", locker.ownerID)
	return locker
}

// Lock attempts to acquire the distributed lock by creating the ephemeral lock
// znode.
func (l *ZookeeperLocker) Lock(ctx context.Context) error {
	defer l.st.lockOp()()

	if l.st.isHeld() {
		return fmt.Errorf("%w (zookeeper path=%s)", ErrLockAlreadyHeld, l.lockPath)
	}

	token := newToken()

	_, err := l.conn.Create(l.lockPath, []byte(token), zk.FlagEphemeral, zk.WorldACL(zk.PermAll))
	if err == zk.ErrNoNode {
		// The parent path does not exist yet. Create the ancestors as
		// persistent nodes and retry once.
		if perr := l.ensureParents(); perr != nil {
			l.logger.Error("failed to create parent nodes", "path", l.lockPath, "error", perr)
			return fmt.Errorf("distlock/zookeeper: create parents: %w", perr)
		}
		_, err = l.conn.Create(l.lockPath, []byte(token), zk.FlagEphemeral, zk.WorldACL(zk.PermAll))
	}
	if err == zk.ErrNodeExists {
		l.logger.Warn("lock is already held", "path", l.lockPath)
		return fmt.Errorf("%w (zookeeper path=%s)", ErrLockAlreadyHeld, l.lockPath)
	}
	if err != nil {
		l.logger.Error("failed to acquire lock", "path", l.lockPath, "error", err)
		return fmt.Errorf("distlock/zookeeper: create: %w", err)
	}

	l.st.markHeld(token, ctx, l.lockTimeout/2, nil)

	l.logger.Info("Lock acquired", "path", l.lockPath, "ownerID", l.ownerID)
	return nil
}

// ensureParents creates every ancestor of lockPath as a persistent node,
// ignoring the ones that already exist.
func (l *ZookeeperLocker) ensureParents() error {
	parts := strings.Split(strings.Trim(l.lockPath, "/"), "/")
	if len(parts) < 2 {
		// Only the root is a parent, and it always exists.
		return nil
	}
	current := ""
	for _, part := range parts[:len(parts)-1] {
		current += "/" + part
		if _, err := l.conn.Create(current, []byte{}, 0, zk.WorldACL(zk.PermAll)); err != nil && err != zk.ErrNodeExists {
			return err
		}
	}
	return nil
}

// Unlock deletes the lock znode, but only if it still carries our token.
//
// It is idempotent and safe against having lost the lock: the node is re-read
// first, and the delete uses the version returned by that read as a CAS, so it
// can only ever delete the exact node version we just inspected.
func (l *ZookeeperLocker) Unlock(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.clearHeld()
	if !held {
		return fmt.Errorf("%w (zookeeper path=%s)", ErrLockNotHeld, l.lockPath)
	}

	data, stat, err := l.conn.Get(l.lockPath)
	if err == zk.ErrNoNode {
		l.logger.Warn("lock node is gone, nothing to release", "path", l.lockPath)
		return fmt.Errorf("%w (zookeeper path=%s: node gone)", ErrLockNotHeld, l.lockPath)
	}
	if err != nil {
		l.logger.Error("failed to read lock node", "path", l.lockPath, "error", err)
		return fmt.Errorf("distlock/zookeeper: get: %w", err)
	}
	if string(data) != token {
		l.logger.Warn("lock node is not ours any more, leaving it untouched", "path", l.lockPath)
		return fmt.Errorf("%w (zookeeper path=%s: taken over)", ErrLockNotHeld, l.lockPath)
	}

	if err := l.conn.Delete(l.lockPath, stat.Version); err != nil {
		l.logger.Error("failed to release lock", "path", l.lockPath, "error", err)
		return fmt.Errorf("distlock/zookeeper: delete: %w", err)
	}

	l.logger.Info("Lock released", "path", l.lockPath, "ownerID", l.ownerID)
	return nil
}

// Renew verifies that we still own the ephemeral lock node.
//
// There is nothing to extend: ZooKeeper keeps an ephemeral node alive for as
// long as the session lives, and the session is kept alive by the client's
// heartbeat. This method therefore only reports whether we still own the node.
func (l *ZookeeperLocker) Renew(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.heldToken()
	if !held {
		return fmt.Errorf("%w (zookeeper path=%s)", ErrLockNotHeld, l.lockPath)
	}

	data, _, err := l.conn.Get(l.lockPath)
	if err == zk.ErrNoNode {
		return fmt.Errorf("%w (zookeeper path=%s: node gone)", ErrLockNotHeld, l.lockPath)
	}
	if err != nil {
		return fmt.Errorf("distlock/zookeeper: get: %w", err)
	}
	if string(data) != token {
		return fmt.Errorf("%w (zookeeper path=%s: taken over)", ErrLockNotHeld, l.lockPath)
	}
	return nil
}

// Close closes the ZooKeeper connection this locker owns, which ends the
// session and therefore makes the ephemeral lock node disappear.
func (l *ZookeeperLocker) Close() error {
	defer l.st.lockOp()()
	l.st.clearHeld()
	l.conn.Close()
	return nil
}
