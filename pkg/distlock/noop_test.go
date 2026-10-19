package distlock

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

// TestNoopLockerLockThenUnlockDoesNotPanic pins the crash that the previous
// implementation had: Unlock() set l.renewTicker to nil while the renewal
// goroutine evaluated l.renewTicker.C in its select. As soon as Unlock won that
// race (which is the common case, because the goroutine is not scheduled until
// after Unlock returns) the goroutine dereferenced a nil *time.Ticker and the
// whole process died with SIGSEGV.
//
// Before the fix this test crashed the test binary; see the report.
func TestNoopLockerLockThenUnlockDoesNotPanic(t *testing.T) {
	l := NewNoopLocker(WithLockTimeout(time.Second))
	ctx := context.Background()

	for i := 0; i < 500; i++ {
		if err := l.Lock(ctx); err != nil {
			t.Fatalf("iteration %d: Lock() = %v, want nil", i, err)
		}
		if err := l.Unlock(ctx); err != nil {
			t.Fatalf("iteration %d: Unlock() = %v, want nil", i, err)
		}
	}
}

// TestNoopLockerDoesNotLeakGoroutines pins the second defect: the renewal
// goroutine never exits because the package never closes stopChan and
// Ticker.Stop() does not close the ticker channel.
func TestNoopLockerDoesNotLeakGoroutines(t *testing.T) {
	base := runtime.NumGoroutine()
	l := NewNoopLocker(WithLockTimeout(time.Second))
	ctx := context.Background()

	const iterations = 200
	for i := 0; i < iterations; i++ {
		if err := l.Lock(ctx); err != nil {
			t.Fatalf("Lock() = %v, want nil", err)
		}
		// Give the renewal goroutine a chance to reach its select, so this test
		// exercises the leak (a select blocked forever on a stopped ticker)
		// rather than the nil dereference the test above covers.
		time.Sleep(time.Millisecond)
		if err := l.Unlock(ctx); err != nil {
			t.Fatalf("Unlock() = %v, want nil", err)
		}
	}

	waitForGoroutines(t, base, iterations)
}

// TestNoopLockerUnlockIsIdempotent asserts that calling Unlock twice is safe:
// no panic, and the second call reports that the lock is not held.
func TestNoopLockerUnlockIsIdempotent(t *testing.T) {
	l := NewNoopLocker(WithLockTimeout(time.Second))
	ctx := context.Background()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	if err := l.Unlock(ctx); err != nil {
		t.Fatalf("first Unlock() = %v, want nil", err)
	}
	if err := l.Unlock(ctx); !errors.Is(err, ErrLockNotHeld) {
		t.Fatalf("second Unlock() = %v, want ErrLockNotHeld", err)
	}
	if err := l.Unlock(ctx); !errors.Is(err, ErrLockNotHeld) {
		t.Fatalf("third Unlock() = %v, want ErrLockNotHeld", err)
	}
}

// TestNoopLockerRenewWithoutLock asserts Renew reports the lock is not held.
func TestNoopLockerRenewWithoutLock(t *testing.T) {
	l := NewNoopLocker(WithLockTimeout(time.Second))

	if err := l.Renew(context.Background()); !errors.Is(err, ErrLockNotHeld) {
		t.Fatalf("Renew() = %v, want ErrLockNotHeld", err)
	}
}

// TestNoopLockerDoubleLock asserts a second Lock on an instance that already
// holds the lock is rejected instead of silently minting a second token.
func TestNoopLockerDoubleLock(t *testing.T) {
	l := NewNoopLocker(WithLockTimeout(time.Second))
	ctx := context.Background()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	if err := l.Lock(ctx); !errors.Is(err, ErrLockAlreadyHeld) {
		t.Fatalf("second Lock() = %v, want ErrLockAlreadyHeld", err)
	}
	if err := l.Unlock(ctx); err != nil {
		t.Fatalf("Unlock() = %v, want nil", err)
	}
}

// TestNoopLockerInvalidTimeoutDoesNotPanic asserts an invalid lock timeout is
// clamped instead of reaching time.NewTicker (which panics for a non-positive
// duration: "non-positive interval for NewTicker").
func TestNoopLockerInvalidTimeoutDoesNotPanic(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1, time.Millisecond, 999 * time.Millisecond} {
		t.Run(timeout.String(), func(t *testing.T) {
			l := NewNoopLocker(WithLockTimeout(timeout))
			ctx := context.Background()
			if err := l.Lock(ctx); err != nil {
				t.Fatalf("Lock() = %v, want nil", err)
			}
			if err := l.Unlock(ctx); err != nil {
				t.Fatalf("Unlock() = %v, want nil", err)
			}
		})
	}
}

// TestNoopLockerRenewalSurvivesCallerContextCancel asserts the renewal loop is
// not tied to the caller's context: the classic `defer cancel()` right after
// acquiring the lock must not stop the renewal (which used to let the lock
// silently expire under the holder's feet).
func TestNoopLockerRenewalSurvivesCallerContextCancel(t *testing.T) {
	log := &countingLogger{}
	l := NewNoopLocker(WithLockTimeout(2*time.Second), WithLogger(log))
	ctx, cancel := context.WithCancel(context.Background())

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	// The renewal interval is lockTimeout/2 == 1s.
	time.Sleep(300 * time.Millisecond)
	cancel() // the classic `defer cancel()` right after acquiring

	time.Sleep(1200 * time.Millisecond) // at least one renewal tick
	if got := log.count("Lock renewed"); got == 0 {
		t.Fatalf("renewal goroutine did not renew after the caller cancelled its context (renewals=%d)", got)
	}
	if err := l.Unlock(context.Background()); err != nil {
		t.Fatalf("Unlock() = %v, want nil", err)
	}
}

// TestNoopLockerCloseStopsRenewal ensures Close releases the renewal goroutine
// and is idempotent.
func TestNoopLockerCloseStopsRenewal(t *testing.T) {
	base := runtime.NumGoroutine()
	l := NewNoopLocker(WithLockTimeout(time.Second))
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
}

// noopHarness wires NoopLocker into the generic lifecycle suite.
//
// NoopLocker instances share no state, so the cross-instance tests
// (mutual exclusion, takeover) are skipped for it: shared=false and
// expireLease=nil.
func noopHarness(t *testing.T) *lockerHarness {
	t.Helper()
	return &lockerHarness{
		newLocker: func() testLocker {
			return NewNoopLocker(WithLockTimeout(10 * time.Second))
		},
		shared: false,
	}
}

// TestNewTokenIsUnique makes sure the ownership token really is fresh on every
// acquisition (the whole safety model rests on this).
func TestNewTokenIsUnique(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		tok := newToken()
		if tok == "" {
			t.Fatal("newToken() returned an empty token")
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("newToken() produced a duplicate after %d calls: %q", i, tok)
		}
		seen[tok] = struct{}{}
	}
}
