package distlock

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/LiangNing7/goutils/pkg/logger"
)

// GORMLocker provides a distributed locking mechanism using GORM (MySQL,
// PostgreSQL, SQLite, ...).
//
// Design:
//
//   - The lock row is identified by its unique "name" column, and ownership is
//     recorded in the new "owner_token" column.
//
//   - Acquisition is two independent, self-contained statements:
//
//     1. UPDATE locks SET owner_token = ?, owner_id = ?, expired_at = ?
//     WHERE name = ? AND (expired_at <= ? OR owner_token IS NULL OR
//     owner_token = the empty string).
//     RowsAffected > 0 means we stole an expired (or never claimed) row.
//
//     2. If nothing was stolen, INSERT ... ON CONFLICT (name) DO NOTHING.
//     RowsAffected == 1 means we created the row and own the lock;
//     0 means somebody else holds it.
//
//     Both steps rely on the database to decide the winner; there is no
//     read-then-write window (the previous implementation did SELECT-then-SAVE
//     inside a transaction and simply could not tell two concurrent winners
//     apart, and it used a bare INSERT whose duplicate-key error aborted the
//     whole PostgreSQL transaction with 25P02, making the "expired lock" path
//     unusable on PostgreSQL).
//
//   - ON CONFLICT DO NOTHING (GORM's clause.OnConflict{DoNothing: true}) is what
//     keeps a duplicate key from poisoning anything: no error is raised and the
//     two statements do not even share a transaction.
//
//   - Unlock and Renew are conditional on owner_token, so a locker whose lease
//     expired can no longer delete or extend the new owner's row.
type GORMLocker struct {
	db          *gorm.DB
	lockName    string
	lockTimeout time.Duration
	ownerID     string
	logger      logger.Logger

	st lockerState
}

// Lock represents a database record for a distributed lock.
//
// OwnerToken was added in the token rework; AutoMigrate adds the column to
// existing tables. Rows created by an older version have an empty owner_token,
// which the acquisition UPDATE treats as "unclaimed" — so after a rolling
// upgrade existing rows are simply stolen once and then behave normally.
type Lock struct {
	ID         uint   `gorm:"primarykey"`
	Name       string `gorm:"unique"`
	OwnerID    string
	OwnerToken string `gorm:"size:64;index"`
	ExpiredAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Ensure GORMLocker implements the Locker and Closer interfaces.
var (
	_ Locker = (*GORMLocker)(nil)
	_ Closer = (*GORMLocker)(nil)
)

// NewGORMLocker initializes a new GORMLocker instance and migrates the locks
// table.
//
// A lock timeout below MinLockTimeout is rejected with ErrInvalidLockTimeout.
func NewGORMLocker(db *gorm.DB, opts ...Option) (*GORMLocker, error) {
	o := ApplyOptions(opts...)
	if err := o.validateErr(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("distlock/gorm: db must not be nil")
	}

	if err := db.AutoMigrate(&Lock{}); err != nil {
		return nil, fmt.Errorf("distlock/gorm: automigrate: %w", err)
	}

	locker := &GORMLocker{
		db:          db,
		lockName:    o.lockName,
		lockTimeout: o.lockTimeout,
		ownerID:     o.ownerID,
		logger:      o.logger,
	}

	locker.logger.Info("GORMLocker initialized", "lockName", locker.lockName, "ownerID", locker.ownerID)

	return locker, nil
}

// Lock acquires the distributed lock.
func (l *GORMLocker) Lock(ctx context.Context) error {
	defer l.st.lockOp()()

	if l.st.isHeld() {
		return fmt.Errorf("%w (gorm name=%s)", ErrLockAlreadyHeld, l.lockName)
	}

	now := time.Now()
	expiredAt := now.Add(l.lockTimeout)
	token := newToken()

	// Step 1: steal a row that is expired or has never been claimed. A single
	// conditional UPDATE is atomic: the RowsAffected count is the verdict.
	steal := l.db.WithContext(ctx).
		Model(&Lock{}).
		Where("name = ? AND (expired_at <= ? OR owner_token IS NULL OR owner_token = '')", l.lockName, now).
		Updates(map[string]any{
			"owner_id":    l.ownerID,
			"owner_token": token,
			"expired_at":  expiredAt,
		})
	if steal.Error != nil {
		l.logger.Error("failed to steal expired lock", "name", l.lockName, "error", steal.Error)
		return fmt.Errorf("distlock/gorm: update: %w", steal.Error)
	}

	if steal.RowsAffected == 0 {
		// Step 2: nothing to steal, so the row either does not exist or is live.
		// ON CONFLICT DO NOTHING lets the database tell us which, without ever
		// raising a duplicate-key error, and in its own statement so no aborted
		// transaction can poison what follows.
		row := Lock{
			Name:       l.lockName,
			OwnerID:    l.ownerID,
			OwnerToken: token,
			ExpiredAt:  expiredAt,
		}
		insert := l.db.WithContext(ctx).
			Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "name"}}, DoNothing: true}).
			Create(&row)
		if insert.Error != nil {
			l.logger.Error("failed to insert lock", "name", l.lockName, "error", insert.Error)
			return fmt.Errorf("distlock/gorm: insert: %w", insert.Error)
		}
		if insert.RowsAffected == 0 {
			l.logger.Warn("lock is already held", "name", l.lockName)
			return fmt.Errorf("%w (gorm name=%s)", ErrLockAlreadyHeld, l.lockName)
		}
	}

	l.st.markHeld(token, ctx, l.lockTimeout/2, func(rctx context.Context) {
		if err := l.renewWithToken(rctx, token); err != nil {
			l.logger.Error("failed to renew lock", "name", l.lockName, "error", err)
		}
	})

	l.logger.Info("Lock acquired", "name", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Unlock releases the distributed lock, but only if the row still carries our
// token.
func (l *GORMLocker) Unlock(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.clearHeld()
	if !held {
		return fmt.Errorf("%w (gorm name=%s)", ErrLockNotHeld, l.lockName)
	}

	res := l.db.WithContext(ctx).
		Where("name = ? AND owner_token = ?", l.lockName, token).
		Delete(&Lock{})
	if res.Error != nil {
		l.logger.Error("failed to release lock", "name", l.lockName, "error", res.Error)
		return fmt.Errorf("distlock/gorm: delete: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		l.logger.Warn("lock is not ours any more, leaving it untouched", "name", l.lockName)
		return fmt.Errorf("%w (gorm name=%s: lease expired or taken over)", ErrLockNotHeld, l.lockName)
	}

	l.logger.Info("Lock released", "name", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Renew refreshes the lease, but only while the row still carries our token.
func (l *GORMLocker) Renew(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.heldToken()
	if !held {
		return fmt.Errorf("%w (gorm name=%s)", ErrLockNotHeld, l.lockName)
	}
	return l.renewWithToken(ctx, token)
}

// renewWithToken performs the conditional lease extension. It never takes the
// operation lock: it is called from Renew (which holds it) and from the renewal
// goroutine (which must not, see lockerState).
func (l *GORMLocker) renewWithToken(ctx context.Context, token string) error {
	res := l.db.WithContext(ctx).
		Model(&Lock{}).
		Where("name = ? AND owner_token = ?", l.lockName, token).
		Update("expired_at", time.Now().Add(l.lockTimeout))
	if res.Error != nil {
		return fmt.Errorf("distlock/gorm: renew: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w (gorm name=%s: lease expired or taken over)", ErrLockNotHeld, l.lockName)
	}
	return nil
}

// Close stops the renewal goroutine. It does not close the *gorm.DB, which the
// caller owns and may share with the rest of the application.
func (l *GORMLocker) Close() error {
	defer l.st.lockOp()()
	l.st.clearHeld()
	return nil
}
