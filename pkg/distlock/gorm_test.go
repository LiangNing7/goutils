package distlock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const gormTestName = "unit-test-lock"

// newTestGORMDB opens a private in-memory SQLite database.
//
// The SQLite driver is already part of this module's dependency graph (it is
// pulled in transitively by the casbin/gorm-adapter chain), so using it here adds
// no new module to go.mod. It gives the GORM locker a *real* database engine to
// arbitrate concurrent acquisitions, which is the strongest end-to-end evidence
// in this suite.
func newTestGORMDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("resolving sql.DB: %v", err)
	}
	// A single connection avoids "database is locked" between the two
	// statements of an acquisition during the concurrency test.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func gormHarness(t *testing.T) *lockerHarness {
	t.Helper()
	db := newTestGORMDB(t)
	return &lockerHarness{
		newLocker: func() testLocker {
			l, err := NewGORMLocker(db,
				WithLockName(gormTestName),
				WithLockTimeout(10*time.Second),
				WithLogger(quietLogger()),
			)
			if err != nil {
				t.Fatalf("NewGORMLocker() = %v", err)
			}
			return l
		},
		expireLease: func() {
			res := db.Model(&Lock{}).
				Where("name = ?", gormTestName).
				Update("expired_at", time.Now().Add(-time.Second))
			if res.Error != nil || res.RowsAffected == 0 {
				t.Fatalf("forcing expiry: err=%v rows=%d", res.Error, res.RowsAffected)
			}
		},
		shared: true,
	}
}

// TestGORMFirstAcquisitionSucceeds: the old implementation could not acquire at
// all on a fresh table on PostgreSQL (the duplicate key aborted the transaction).
func TestGORMFirstAcquisitionSucceeds(t *testing.T) {
	db := newTestGORMDB(t)
	ctx := context.Background()
	l, err := NewGORMLocker(db, WithLockName(gormTestName), WithLockTimeout(10*time.Second), WithLogger(quietLogger()))
	if err != nil {
		t.Fatalf("NewGORMLocker() = %v", err)
	}
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("first Lock() = %v, want nil", err)
	}

	var rows []Lock
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("reading locks: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("lock rows = %d, want 1", len(rows))
	}
	if rows[0].OwnerToken == "" {
		t.Fatal("owner_token is empty after Lock()")
	}
}

// legacyLock mirrors the Lock struct as it existed before the token rework: no
// owner_token column.
type legacyLock struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"unique"`
	OwnerID   string
	ExpiredAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (legacyLock) TableName() string { return "locks" }

// TestGORMAutoMigrateAddsOwnerTokenColumn asserts the new column is created on an
// existing table, which is what a rolling upgrade depends on.
func TestGORMAutoMigrateAddsOwnerTokenColumn(t *testing.T) {
	db := newTestGORMDB(t)

	// Simulate a pre-token deployment by creating the old table with the old
	// schema and a row in it.
	if err := db.AutoMigrate(&legacyLock{}); err != nil {
		t.Fatalf("creating the legacy table: %v", err)
	}
	if err := db.Exec(`INSERT INTO locks (name, owner_id, expired_at) VALUES (?, ?, ?)`,
		gormTestName, "legacy-owner", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("seeding legacy row: %v", err)
	}
	if db.Migrator().HasColumn(&Lock{}, "OwnerToken") {
		t.Fatal("precondition failed: the legacy table already has owner_token")
	}

	l, err := NewGORMLocker(db, WithLockName(gormTestName), WithLockTimeout(10*time.Second), WithLogger(quietLogger()))
	if err != nil {
		t.Fatalf("NewGORMLocker() on a legacy table = %v, want nil", err)
	}
	defer func() { _ = l.Close() }()

	if !db.Migrator().HasColumn(&Lock{}, "OwnerToken") {
		t.Fatal("AutoMigrate did not add the owner_token column")
	}

	// A legacy row has an empty owner_token, which counts as unclaimed, so the
	// locker takes it over instead of being stuck behind it.
	if err := l.Lock(context.Background()); err != nil {
		t.Fatalf("Lock() over a legacy row = %v, want nil", err)
	}
	var row Lock
	if err := db.First(&row, "name = ?", gormTestName).Error; err != nil {
		t.Fatalf("reading the row: %v", err)
	}
	if row.OwnerToken == "" {
		t.Fatal("the legacy row was not claimed (owner_token still empty)")
	}
	if row.OwnerID != l.ownerID {
		t.Fatalf("owner_id = %q, want %q", row.OwnerID, l.ownerID)
	}
}

// TestGORMRenewIsConditional asserts Renew cannot extend somebody else's row.
func TestGORMRenewIsConditional(t *testing.T) {
	db := newTestGORMDB(t)
	ctx := context.Background()
	a, err := NewGORMLocker(db, WithLockName(gormTestName), WithLockTimeout(10*time.Second), WithLogger(quietLogger()))
	if err != nil {
		t.Fatalf("NewGORMLocker(a) = %v", err)
	}
	b, err := NewGORMLocker(db, WithLockName(gormTestName), WithLockTimeout(10*time.Second), WithLogger(quietLogger()))
	if err != nil {
		t.Fatalf("NewGORMLocker(b) = %v", err)
	}
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()

	if err := a.Lock(ctx); err != nil {
		t.Fatalf("a.Lock() = %v, want nil", err)
	}
	// Expire a's row and let b take over.
	if err := db.Model(&Lock{}).Where("name = ?", gormTestName).
		Update("expired_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("expiring a's row: %v", err)
	}
	if err := b.Lock(ctx); err != nil {
		t.Fatalf("b.Lock() = %v, want nil", err)
	}

	var before Lock
	if err := db.First(&before, "name = ?", gormTestName).Error; err != nil {
		t.Fatalf("reading row: %v", err)
	}

	if err := a.Renew(ctx); !errors.Is(err, ErrLockNotHeld) {
		t.Fatalf("a.Renew() = %v, want ErrLockNotHeld", err)
	}
	if err := a.Unlock(ctx); !errors.Is(err, ErrLockNotHeld) {
		t.Fatalf("a.Unlock() = %v, want ErrLockNotHeld", err)
	}

	var after Lock
	if err := db.First(&after, "name = ?", gormTestName).Error; err != nil {
		t.Fatalf("b's row disappeared after a.Unlock(): %v", err)
	}
	if after.OwnerToken != before.OwnerToken {
		t.Fatalf("b's token changed: before=%q after=%q", before.OwnerToken, after.OwnerToken)
	}
	if !after.ExpiredAt.Equal(before.ExpiredAt) {
		t.Fatalf("a.Renew() modified b's lease: before=%s after=%s", before.ExpiredAt, after.ExpiredAt)
	}
}

// TestGORMConcurrentLockUsesTheDatabaseAsArbiter races many locker instances
// against a real SQLite database. Exactly one must win, which is the property
// the old SELECT-then-SAVE transaction could not provide.
func TestGORMConcurrentLockUsesTheDatabaseAsArbiter(t *testing.T) {
	db := newTestGORMDB(t)
	ctx := context.Background()
	const contenders = 24

	lockers := make([]*GORMLocker, contenders)
	for i := range lockers {
		l, err := NewGORMLocker(db, WithLockName(gormTestName), WithLockTimeout(10*time.Second), WithLogger(quietLogger()))
		if err != nil {
			t.Fatalf("NewGORMLocker(%d) = %v", i, err)
		}
		defer func() { _ = l.Close() }()
		lockers[i] = l
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []int
	)
	start := make(chan struct{})
	for i := range lockers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := lockers[i].Lock(ctx); err == nil {
				mu.Lock()
				winners = append(winners, i)
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("got %d winners (%v), want exactly 1", len(winners), winners)
	}
	var rows []Lock
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("reading rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("lock rows = %d, want 1", len(rows))
	}
	if err := lockers[winners[0]].Unlock(ctx); err != nil {
		t.Fatalf("winner.Unlock() = %v, want nil", err)
	}
}
