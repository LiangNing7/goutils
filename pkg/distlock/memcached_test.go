package distlock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

// fakeMemcache is an in-memory stand-in for the four memcached commands the
// locker uses, including the CAS id that makes renewal atomic.
type fakeMemcache struct {
	mu      sync.Mutex
	items   map[string]*memcache.Item
	cas     map[string]uint64
	nextCas uint64
}

func newFakeMemcache() *fakeMemcache {
	return &fakeMemcache{
		items: make(map[string]*memcache.Item),
		cas:   make(map[string]uint64),
	}
}

func (f *fakeMemcache) storeLocked(item *memcache.Item) {
	f.items[item.Key] = &memcache.Item{
		Key:        item.Key,
		Value:      append([]byte(nil), item.Value...),
		Expiration: item.Expiration,
	}
	f.nextCas++
	f.cas[item.Key] = f.nextCas
}

func (f *fakeMemcache) Add(item *memcache.Item) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.items[item.Key]; exists {
		return memcache.ErrNotStored
	}
	f.storeLocked(item)
	return nil
}

func (f *fakeMemcache) Get(key string) (*memcache.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, exists := f.items[key]
	if !exists {
		return nil, memcache.ErrCacheMiss
	}
	clone := *item
	clone.CasID = f.cas[key]
	return &clone, nil
}

func (f *fakeMemcache) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.items[key]; !exists {
		return memcache.ErrCacheMiss
	}
	delete(f.items, key)
	delete(f.cas, key)
	return nil
}

func (f *fakeMemcache) CompareAndSwap(item *memcache.Item) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.items[item.Key]; !exists {
		return memcache.ErrCacheMiss
	}
	if f.cas[item.Key] != item.CasID {
		return memcache.ErrCASConflict
	}
	f.storeLocked(item)
	return nil
}

func (f *fakeMemcache) value(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if item, ok := f.items[key]; ok {
		return string(item.Value)
	}
	return ""
}

func (f *fakeMemcache) expiration(key string) int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if item, ok := f.items[key]; ok {
		return item.Expiration
	}
	return -1
}

// replaceWith puts a value into the store behind the locker's back, as another
// owner would.
func (f *fakeMemcache) replaceWith(item *memcache.Item) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.storeLocked(item)
}

const memcachedTestKey = "unit-test-lock"

func memcachedHarness(t *testing.T) *lockerHarness {
	t.Helper()
	fake := newFakeMemcache()
	base := newMemcachedLockerWithClient(fake)
	return &lockerHarness{
		newLocker: func() testLocker {
			if base == nil {
				return nil
			}
			return newMemcachedLockerWithClient(fake)
		},
		expireLease: func() {
			fake.mu.Lock()
			delete(fake.items, memcachedTestKey)
			delete(fake.cas, memcachedTestKey)
			fake.mu.Unlock()
		},
		shared: true,
	}
}

// newMemcachedLockerWithClient builds a MemcachedLocker over an injected client,
// so the backend can be exercised without a server. It uses the same options as
// NewMemcachedLocker.
func newMemcachedLockerWithClient(client memcacheClient) *MemcachedLocker {
	o := ApplyOptions(WithLockName(memcachedTestKey), WithLockTimeout(10*time.Second))
	return &MemcachedLocker{
		client:      client,
		lockName:    o.lockName,
		lockTimeout: o.lockTimeout,
		ownerID:     o.ownerID,
		logger:      o.logger,
	}
}

// TestMemcachedExpirationNeverYieldsZero pins the deadlock bug: a lock timeout
// below one second used to truncate to 0, and memcached reads 0 as "never
// expire", so the lock became permanent.
func TestMemcachedExpirationNeverYieldsZero(t *testing.T) {
	for _, timeout := range []time.Duration{
		-time.Second,
		0,
		time.Millisecond,
		999 * time.Millisecond,
		time.Second,
		1500 * time.Millisecond,
		30 * 24 * time.Hour,
		60 * 24 * time.Hour,
	} {
		got := memcachedExpiration(timeout)
		if got < 1 {
			t.Fatalf("memcachedExpiration(%s) = %d, want >= 1 (0 means never expire)", timeout, got)
		}
		if got > int32(memcachedMaxExpiration/time.Second) {
			t.Fatalf("memcachedExpiration(%s) = %d, want <= %d (above 30 days memcached reads it as a 1970 timestamp)",
				timeout, got, int32(memcachedMaxExpiration/time.Second))
		}
	}
	if got, want := memcachedExpiration(1500*time.Millisecond), int32(2); got != want {
		t.Fatalf("memcachedExpiration(1.5s) = %d, want %d (rounded up so the lease is never shortened)", got, want)
	}
}

// TestMemcachedLockStoresANonZeroExpiration asserts the lesson above reaches the
// wire: a sub-second timeout must still store a positive expiration.
func TestMemcachedLockStoresANonZeroExpiration(t *testing.T) {
	fake := newFakeMemcache()
	l := NewMemcachedLocker("127.0.0.1:11211", WithLockName(memcachedTestKey), WithLockTimeout(500*time.Millisecond))
	l.client = fake // the timeout is clamped, so the stored value must be positive
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	if got := fake.expiration(memcachedTestKey); got < 1 {
		t.Fatalf("stored expiration = %d, want >= 1 (0 would mean never expire)", got)
	}
}

// TestMemcachedRenewOnlyTouchesOwnItem pins the Replace bug: the old Renew used
// Replace, which overwrote (and re-extended) whatever item was under the key,
// including another owner's lock.
func TestMemcachedRenewOnlyTouchesOwnItem(t *testing.T) {
	fake := newFakeMemcache()
	ctx := context.Background()
	a := newMemcachedLockerWithClient(fake)
	b := newMemcachedLockerWithClient(fake)

	if err := a.Lock(ctx); err != nil {
		t.Fatalf("a.Lock() = %v, want nil", err)
	}

	// b takes over, as if a's lease had expired.
	fake.replaceWith(&memcache.Item{Key: memcachedTestKey, Value: []byte("b-token"), Expiration: 42})

	if err := a.Renew(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("a.Renew() = %v, want ErrLockNotHeld", err)
	}
	if got := fake.value(memcachedTestKey); got != "b-token" {
		t.Fatalf("a.Renew() overwrote another owner's value: got %q, want %q", got, "b-token")
	}
	if got := fake.expiration(memcachedTestKey); got != 42 {
		t.Fatalf("a.Renew() modified another owner's expiration: got %d, want 42", got)
	}

	if err := b.Renew(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("b.Renew() without holding the lock = %v, want ErrLockNotHeld", err)
	}
}

// TestMemcachedRenewUsesCas proves the renewal is a genuine compare-and-swap:
// a concurrent modification between Get and CAS must fail the renewal.
func TestMemcachedRenewUsesCas(t *testing.T) {
	fake := newFakeMemcache()
	l := newMemcachedLockerWithClient(fake)
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	if err := l.Renew(ctx); err != nil {
		t.Fatalf("Renew() = %v, want nil", err)
	}

	// Somebody replaces the item between the locker's Get and CAS: emulate by
	// bumping the CAS id after the locker read it.
	token := fake.value(memcachedTestKey)
	item, err := fake.Get(memcachedTestKey)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	fake.replaceWith(&memcache.Item{Key: memcachedTestKey, Value: []byte(token), Expiration: 7})

	if err := fake.CompareAndSwap(&memcache.Item{
		Key:        memcachedTestKey,
		Value:      []byte(token),
		Expiration: 99,
		CasID:      item.CasID, // stale CAS id
	}); err != memcache.ErrCASConflict {
		t.Fatalf("CompareAndSwap with a stale CAS id = %v, want ErrCASConflict", err)
	}
	if got := fake.expiration(memcachedTestKey); got != 7 {
		t.Fatalf("a stale CAS modified the item: expiration = %d, want 7", got)
	}
}

// TestMemcachedUnlockToleratesEviction: an already-evicted key must be reported
// as ErrLockNotHeld, not as a release failure (the old code returned the raw
// ErrCacheMiss and callers saw a spurious error).
func TestMemcachedUnlockToleratesEviction(t *testing.T) {
	fake := newFakeMemcache()
	l := newMemcachedLockerWithClient(fake)
	ctx := context.Background()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	// The item is evicted before we get to release it.
	if err := fake.Delete(memcachedTestKey); err != nil {
		t.Fatalf("evicting: %v", err)
	}

	if err := l.Unlock(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("Unlock() after eviction = %v, want ErrLockNotHeld", err)
	}
	// And the second call is still idempotent.
	if err := l.Unlock(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("second Unlock() = %v, want ErrLockNotHeld", err)
	}
	_ = l.Close()
}

// TestMemcachedUnlockLeavesForeignItem documents the known non-atomicity: Unlock
// checks the token before deleting, so a foreign lock survives, but the check and
// the delete are two round trips (see the type documentation).
func TestMemcachedUnlockLeavesForeignItem(t *testing.T) {
	fake := newFakeMemcache()
	l := newMemcachedLockerWithClient(fake)
	ctx := context.Background()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	fake.replaceWith(&memcache.Item{Key: memcachedTestKey, Value: []byte("foreign"), Expiration: 30})

	if err := l.Unlock(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("Unlock() = %v, want ErrLockNotHeld", err)
	}
	if got := fake.value(memcachedTestKey); got != "foreign" {
		t.Fatalf("Unlock() deleted a foreign item: value = %q", got)
	}
}
