package distlock

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

// testLocker is what every harness returns: the Locker contract plus the
// optional Closer every concrete type in this package implements.
type testLocker interface {
	Locker
	Closer
}

// lockerHarness describes how to build lockers backed by one (usually faked)
// backend, so the generic lifecycle tests can run against every backend.
type lockerHarness struct {
	// newLocker returns a fresh locker instance over the shared backend.
	newLocker func() testLocker
	// expireLease makes the lock currently held through this backend look
	// expired, simulating a lease running out while the holder is still alive
	// (the "A's TTL expires, B acquires, A calls Unlock" scenario).
	expireLease func()
	// shared is false for backends where two instances cannot contend because
	// there is no shared state at all (NoopLocker).
	shared bool
}

// harnesses returns every backend harness, in a stable order.
func harnesses(t *testing.T) []struct {
	name  string
	build func(*testing.T) *lockerHarness
} {
	t.Helper()
	return []struct {
		name  string
		build func(*testing.T) *lockerHarness
	}{
		{"noop", noopHarness},
		{"redis", redisHarness},
		{"mongo", mongoHarness},
		{"gorm/sqlite", gormHarness},
		{"consul", consulHarness},
		{"zookeeper", zookeeperHarness},
		{"memcached", memcachedHarness},
	}
}

func eachHarness(t *testing.T, fn func(t *testing.T, h *lockerHarness)) {
	t.Helper()
	for _, tc := range harnesses(t) {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fn(t, tc.build(t))
		})
	}
}

// TestLockUnlockRoundTrip is the baseline: acquire, release, and make sure a
// second acquisition on the same instance works.
func TestLockUnlockRoundTrip(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		ctx := context.Background()
		l := h.newLocker()

		if err := l.Lock(ctx); err != nil {
			t.Fatalf("Lock() = %v, want nil", err)
		}
		if err := l.Unlock(ctx); err != nil {
			t.Fatalf("Unlock() = %v, want nil", err)
		}
		if err := l.Lock(ctx); err != nil {
			t.Fatalf("second Lock() = %v, want nil", err)
		}
		if err := l.Unlock(ctx); err != nil {
			t.Fatalf("second Unlock() = %v, want nil", err)
		}
		if err := l.Close(); err != nil {
			t.Fatalf("Close() = %v, want nil", err)
		}
	})
}

// TestUnlockIsIdempotent asserts Unlock can be called repeatedly: no panic, no
// error other than ErrLockNotHeld, and no accidental deletion of a foreign lock.
func TestUnlockIsIdempotent(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		ctx := context.Background()
		l := h.newLocker()

		if err := l.Lock(ctx); err != nil {
			t.Fatalf("Lock() = %v, want nil", err)
		}
		if err := l.Unlock(ctx); err != nil {
			t.Fatalf("first Unlock() = %v, want nil", err)
		}
		for i := 2; i <= 4; i++ {
			if err := l.Unlock(ctx); !errors.Is(err, ErrLockNotHeld) {
				t.Fatalf("Unlock() #%d = %v, want ErrLockNotHeld", i, err)
			}
		}
		_ = l.Close()
	})
}

// TestRenewWithoutLock asserts Renew refuses when the instance holds nothing.
func TestRenewWithoutLock(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		l := h.newLocker()
		if err := l.Renew(context.Background()); !errors.Is(err, ErrLockNotHeld) {
			t.Fatalf("Renew() = %v, want ErrLockNotHeld", err)
		}
		_ = l.Close()
	})
}

// TestDoubleLockOnSameInstance asserts a second Lock while the instance already
// holds the lock does not silently mint a second token.
func TestDoubleLockOnSameInstance(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		ctx := context.Background()
		l := h.newLocker()

		if err := l.Lock(ctx); err != nil {
			t.Fatalf("Lock() = %v, want nil", err)
		}
		if err := l.Lock(ctx); !errors.Is(err, ErrLockAlreadyHeld) {
			t.Fatalf("second Lock() = %v, want ErrLockAlreadyHeld", err)
		}
		if err := l.Unlock(ctx); err != nil {
			t.Fatalf("Unlock() = %v, want nil", err)
		}
		_ = l.Close()
	})
}

// TestNoGoroutineLeakAfterManyCycles is the leak regression test: before the
// fix every acquisition leaked one renewal goroutine forever.
func TestNoGoroutineLeakAfterManyCycles(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		base := runtime.NumGoroutine()
		ctx := context.Background()
		l := h.newLocker()

		const iterations = 200
		for i := 0; i < iterations; i++ {
			if err := l.Lock(ctx); err != nil {
				t.Fatalf("Lock() = %v, want nil", err)
			}
			if err := l.Unlock(ctx); err != nil {
				t.Fatalf("Unlock() = %v, want nil", err)
			}
		}
		if err := l.Close(); err != nil {
			t.Fatalf("Close() = %v, want nil", err)
		}
		waitForGoroutines(t, base, iterations)
	})
}

// TestCloseIsIdempotent asserts Close can be called twice and releases the
// renewal goroutine.
func TestCloseIsIdempotent(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		base := runtime.NumGoroutine()
		l := h.newLocker()

		if err := l.Lock(context.Background()); err != nil {
			t.Fatalf("Lock() = %v, want nil", err)
		}
		if err := l.Close(); err != nil {
			t.Fatalf("Close() = %v, want nil", err)
		}
		if err := l.Close(); err != nil {
			t.Fatalf("second Close() = %v, want nil", err)
		}
		waitForGoroutines(t, base, 1)
	})
}

// TestMutualExclusionBetweenInstances asserts the second contender is rejected
// while the first holds the lock.
func TestMutualExclusionBetweenInstances(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		if !h.shared {
			t.Skip("backend has no shared state, instances cannot contend")
		}
		ctx := context.Background()
		a, b := h.newLocker(), h.newLocker()

		if err := a.Lock(ctx); err != nil {
			t.Fatalf("a.Lock() = %v, want nil", err)
		}
		if err := b.Lock(ctx); !errors.Is(err, ErrLockAlreadyHeld) {
			t.Fatalf("b.Lock() = %v, want ErrLockAlreadyHeld", err)
		}
		// The failed contender must not have stolen anything: a still owns it.
		if err := a.Renew(ctx); err != nil {
			t.Fatalf("a.Renew() = %v, want nil (a must still own the lock)", err)
		}
		if err := a.Unlock(ctx); err != nil {
			t.Fatalf("a.Unlock() = %v, want nil", err)
		}
		// Now b can have it.
		if err := b.Lock(ctx); err != nil {
			t.Fatalf("b.Lock() after a released = %v, want nil", err)
		}
		if err := b.Unlock(ctx); err != nil {
			t.Fatalf("b.Unlock() = %v, want nil", err)
		}
	})
}

// TestExpiredHolderCannotReleaseSuccessor is the central ownership test:
//
//	A acquires -> A's lease expires -> B acquires -> A calls Unlock.
//
// B must still hold the lock afterwards, and A must be told it no longer holds
// it (ErrLockNotHeld). Before the token rework every backend deleted by name and
// destroyed B's lock.
func TestExpiredHolderCannotReleaseSuccessor(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		if !h.shared || h.expireLease == nil {
			t.Skip("backend cannot simulate lease expiry")
		}
		ctx := context.Background()
		a, b := h.newLocker(), h.newLocker()

		if err := a.Lock(ctx); err != nil {
			t.Fatalf("a.Lock() = %v, want nil", err)
		}

		// Simulate the lease running out under A's feet.
		h.expireLease()

		if err := b.Lock(ctx); err != nil {
			t.Fatalf("b.Lock() after a's lease expired = %v, want nil", err)
		}

		// A now tries to release a lock it no longer owns.
		if err := a.Unlock(ctx); !errors.Is(err, ErrLockNotHeld) {
			t.Fatalf("a.Unlock() = %v, want ErrLockNotHeld", err)
		}

		// B must still hold it.
		if err := b.Renew(ctx); err != nil {
			t.Fatalf("b.Renew() after a.Unlock() = %v, want nil (a destroyed b's lock)", err)
		}
		if err := b.Unlock(ctx); err != nil {
			t.Fatalf("b.Unlock() = %v, want nil", err)
		}

		// And A's renewal must not have revived anything either.
		if err := a.Renew(ctx); !errors.Is(err, ErrLockNotHeld) {
			t.Fatalf("a.Renew() = %v, want ErrLockNotHeld", err)
		}
	})
}

// TestConcurrentLockHasExactlyOneWinner races many instances for the same lock.
//
// The mutual exclusion on the fake backends is only as strong as the fake; for
// gorm/sqlite this test runs against a real database that arbitrates, which is
// the strongest evidence in this suite.
func TestConcurrentLockHasExactlyOneWinner(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		if !h.shared {
			t.Skip("backend has no shared state, instances cannot contend")
		}
		ctx := context.Background()
		const contenders = 16

		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			winners []int
			lockers = make([]testLocker, contenders)
		)
		for i := 0; i < contenders; i++ {
			lockers[i] = h.newLocker()
		}

		start := make(chan struct{})
		for i := 0; i < contenders; i++ {
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
		if err := lockers[winners[0]].Unlock(ctx); err != nil {
			t.Fatalf("winner.Unlock() = %v, want nil", err)
		}
	})
}

// TestRenewalKeepsLockAliveAfterCallerContextCancel asserts the renewal goroutine
// is detached from the caller's context, using the backend to observe the lease.
func TestRenewalKeepsLockAliveAfterCallerContextCancel(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		if !h.shared {
			t.Skip("no shared state to observe renewal through")
		}
		ctx, cancel := context.WithCancel(context.Background())
		l := h.newLocker()

		if err := l.Lock(ctx); err != nil {
			t.Fatalf("Lock() = %v, want nil", err)
		}
		cancel() // the classic `defer cancel()`

		// The renewal goroutine must still be alive: Renew has to keep working.
		deadline := time.Now().Add(800 * time.Millisecond)
		for {
			if err := l.Renew(context.Background()); err != nil {
				t.Fatalf("Renew() after caller cancel = %v, want nil", err)
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err := l.Unlock(context.Background()); err != nil {
			t.Fatalf("Unlock() = %v, want nil", err)
		}
		_ = l.Close()
	})
}

// TestInvalidLockTimeoutIsRejected asserts every constructor either rejects or
// clamps an out-of-range timeout instead of panicking in time.NewTicker.
func TestInvalidLockTimeoutIsRejected(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second, time.Millisecond, 999 * time.Millisecond} {
		t.Run(timeout.String(), func(t *testing.T) {
			for _, build := range []struct {
				name string
				run  func(t *testing.T)
			}{
				{"noop", func(t *testing.T) {
					exerciseClampedLocker(t, NewNoopLocker(WithLockTimeout(timeout)))
				}},
				{"redis", func(t *testing.T) {
					exerciseClampedLocker(t, NewRedisLocker(newFakeRedis(), WithLockTimeout(timeout)))
				}},
				{"memcached", func(t *testing.T) {
					l := NewMemcachedLocker("127.0.0.1:11211", WithLockTimeout(timeout))
					t.Cleanup(func() { _ = l.Close() })
					if got := memcachedExpiration(l.lockTimeout); got < 1 {
						t.Fatalf("memcached expiration for %s = %ds, want >= 1s (0 means never expire)", timeout, got)
					}
				}},
				{"gorm", func(t *testing.T) {
					_, err := NewGORMLocker(nil, WithLockTimeout(timeout))
					if !errors.Is(err, ErrInvalidLockTimeout) {
						t.Fatalf("NewGORMLocker() error = %v, want ErrInvalidLockTimeout", err)
					}
				}},
				{"mongo", func(t *testing.T) {
					_, err := NewMongoLocker("mongodb://127.0.0.1:27017", "test", WithLockTimeout(timeout))
					if !errors.Is(err, ErrInvalidLockTimeout) {
						t.Fatalf("NewMongoLocker() error = %v, want ErrInvalidLockTimeout", err)
					}
				}},
				{"consul", func(t *testing.T) {
					_, err := NewConsulLocker("127.0.0.1:8500", WithLockTimeout(timeout))
					if !errors.Is(err, ErrInvalidLockTimeout) {
						t.Fatalf("NewConsulLocker() error = %v, want ErrInvalidLockTimeout", err)
					}
				}},
				{"etcd", func(t *testing.T) {
					_, err := NewEtcdLocker([]string{"127.0.0.1:2379"}, WithLockTimeout(timeout))
					if !errors.Is(err, ErrInvalidLockTimeout) {
						t.Fatalf("NewEtcdLocker() error = %v, want ErrInvalidLockTimeout", err)
					}
				}},
				{"zookeeper", func(t *testing.T) {
					_, err := NewZookeeperLocker([]string{"127.0.0.1:2181"}, WithLockTimeout(timeout))
					if !errors.Is(err, ErrInvalidLockTimeout) {
						t.Fatalf("NewZookeeperLocker() error = %v, want ErrInvalidLockTimeout", err)
					}
				}},
			} {
				t.Run(build.name, build.run)
			}
		})
	}
}

// exerciseClampedLocker drives a full lock/unlock cycle on a locker whose
// timeout was below the minimum, proving the clamped value never reaches
// time.NewTicker as a non-positive duration.
func exerciseClampedLocker(t *testing.T, l testLocker) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic with an out-of-range lock timeout: %v", r)
		}
	}()
	ctx := context.Background()
	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := l.Unlock(ctx); err != nil {
		t.Fatalf("Unlock() = %v, want nil", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}

// TestStaleHolderCannotUnlockSuccessor is the same property as above, observed
// from the losing side instead: after the takeover, the stale holder's Unlock
// must be a no-op for the successor and must be reported as ErrLockNotHeld.
func TestStaleHolderCannotUnlockSuccessor(t *testing.T) {
	eachHarness(t, func(t *testing.T, h *lockerHarness) {
		if !h.shared || h.expireLease == nil {
			t.Skip("backend cannot simulate lease expiry")
		}
		ctx := context.Background()
		a, b := h.newLocker(), h.newLocker()

		if err := a.Lock(ctx); err != nil {
			t.Fatalf("a.Lock() = %v, want nil", err)
		}
		h.expireLease()
		if err := b.Lock(ctx); err != nil {
			t.Fatalf("b.Lock() = %v, want nil", err)
		}

		// A's Unlock and Renew must both leave the successor alone.
		if err := a.Unlock(ctx); !errors.Is(err, ErrLockNotHeld) {
			t.Fatalf("a.Unlock() = %v, want ErrLockNotHeld", err)
		}
		if err := a.Unlock(ctx); !errors.Is(err, ErrLockNotHeld) {
			t.Fatalf("a.Unlock() again = %v, want ErrLockNotHeld", err)
		}
		if err := b.Renew(ctx); err != nil {
			t.Fatalf("b.Renew() = %v, want nil", err)
		}

		// A third contender must still see the lock as held by b.
		c := h.newLocker()
		if err := c.Lock(ctx); !errors.Is(err, ErrLockAlreadyHeld) {
			t.Fatalf("c.Lock() = %v, want ErrLockAlreadyHeld (b still holds it)", err)
		}
		if err := b.Unlock(ctx); err != nil {
			t.Fatalf("b.Unlock() = %v, want nil", err)
		}
	})
}
