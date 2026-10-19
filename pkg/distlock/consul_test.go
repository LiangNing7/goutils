package distlock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
)

var errFakeConsulUnknownSession = errors.New("fake consul: session not found")

// fakeConsul emulates the two Consul primitives the locker depends on:
// session lifetimes and the "acquire only if no live session holds the key"
// rule of KV().Acquire.
type fakeConsul struct {
	mu       sync.Mutex
	pairs    map[string]*api.KVPair
	sessions map[string]bool
	entries  map[string]*api.SessionEntry
	nextID   int
	renewals int
}

func newFakeConsul() *fakeConsul {
	return &fakeConsul{
		pairs:    make(map[string]*api.KVPair),
		sessions: make(map[string]bool),
		entries:  make(map[string]*api.SessionEntry),
	}
}

// expireSessions invalidates every session, as Consul does when a session TTL
// lapses because its owner stopped renewing.
func (f *fakeConsul) expireSessions() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = make(map[string]bool)
}

func (f *fakeConsul) renewCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renewals
}

func (f *fakeConsul) liveSessions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

func (f *fakeConsul) tokenAt(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if pair, ok := f.pairs[key]; ok {
		return string(pair.Value)
	}
	return ""
}

func (f *fakeConsul) lastEntry() *api.SessionEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, entry := range f.entries {
		return entry
	}
	return nil
}

func (f *fakeConsul) Create(se *api.SessionEntry, _ *api.WriteOptions) (string, *api.WriteMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("session-%d", f.nextID)
	f.sessions[id] = true
	f.entries[id] = se
	return id, nil, nil
}

func (f *fakeConsul) Renew(id string, _ *api.WriteOptions) (*api.SessionEntry, *api.WriteMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.sessions[id] {
		return nil, nil, errFakeConsulUnknownSession
	}
	f.renewals++
	return f.entries[id], nil, nil
}

func (f *fakeConsul) Destroy(id string, _ *api.WriteOptions) (*api.WriteMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
	delete(f.entries, id)
	// SessionBehaviorRelease.
	for key, pair := range f.pairs {
		if pair.Session == id {
			delete(f.pairs, key)
		}
	}
	return nil, nil
}

func (f *fakeConsul) Acquire(p *api.KVPair, _ *api.WriteOptions) (bool, *api.WriteMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.pairs[p.Key]; ok && f.sessions[existing.Session] {
		// Held by a session that is still alive: Acquire answers
		// (false, nil, nil) — no error, just "you did not get it".
		return false, nil, nil
	}
	f.pairs[p.Key] = &api.KVPair{
		Key:     p.Key,
		Value:   append([]byte(nil), p.Value...),
		Session: p.Session,
	}
	return true, nil, nil
}

func (f *fakeConsul) Release(p *api.KVPair, _ *api.WriteOptions) (bool, *api.WriteMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.pairs[p.Key]
	if !ok || existing.Session != p.Session || !f.sessions[p.Session] {
		return false, nil, nil
	}
	delete(f.pairs, p.Key)
	return true, nil, nil
}

const consulTestKey = "unit-test-lock"

func consulHarness(t *testing.T) *lockerHarness {
	t.Helper()
	fake := newFakeConsul()
	return &lockerHarness{
		newLocker: func() testLocker {
			o := ApplyOptions(WithLockName(consulTestKey), WithLockTimeout(10*time.Second))
			return newConsulLocker(fake, fake, o)
		},
		expireLease: func() { fake.expireSessions() },
		shared:      true,
	}
}

// TestConsulAcquireBoolIsChecked pins the bug where the return value of
// KV().Acquire was discarded, so every Lock "succeeded" and silently stole the
// key.
func TestConsulAcquireBoolIsChecked(t *testing.T) {
	fake := newFakeConsul()
	ctx := context.Background()
	a := newConsulLocker(fake, fake, ApplyOptions(WithLockName(consulTestKey), WithLockTimeout(10*time.Second)))
	b := newConsulLocker(fake, fake, ApplyOptions(WithLockName(consulTestKey), WithLockTimeout(10*time.Second)))

	if err := a.Lock(ctx); err != nil {
		t.Fatalf("a.Lock() = %v, want nil", err)
	}
	tokenA := fake.tokenAt(consulTestKey)

	if err := b.Lock(ctx); !isErrLockAlreadyHeld(err) {
		t.Fatalf("b.Lock() = %v, want ErrLockAlreadyHeld", err)
	}
	if got := fake.tokenAt(consulTestKey); got != tokenA {
		t.Fatalf("b.Lock() overwrote a's key: %q, want %q", got, tokenA)
	}
	// The loser must also clean up the session it created.
	if got := fake.liveSessions(); got != 1 {
		t.Fatalf("live sessions = %d, want 1 (the loser must destroy its own session)", got)
	}
}

// TestConsulRenewUsesTheSessionID pins the second Consul bug: Session().Renew was
// called with the hostname, which can never be a session id.
func TestConsulRenewUsesTheSessionID(t *testing.T) {
	fake := newFakeConsul()
	l := newConsulLocker(fake, fake, ApplyOptions(WithLockName(consulTestKey), WithLockTimeout(10*time.Second)))
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	if l.sessionID == "" {
		t.Fatal("Lock() did not record the session id")
	}
	if err := l.Renew(ctx); err != nil {
		t.Fatalf("Renew() = %v, want nil (the session id must be used)", err)
	}
	if got := fake.renewCount(); got != 1 {
		t.Fatalf("session renewals = %d, want 1", got)
	}
}

// TestConsulTTLIsRoundedUpToWholeSeconds documents the TTL rendering: Consul
// session TTLs are whole seconds, so a timeout must be rounded up to never
// shorten the lease.
func TestConsulTTLIsRoundedUpToWholeSeconds(t *testing.T) {
	for timeout, want := range map[time.Duration]string{
		10 * time.Second:        "10s",
		1500 * time.Millisecond: "2s",
		100 * time.Millisecond:  "1s", // defensive; the constructor rejects this
		-time.Second:            "1s", // defensive; the constructor rejects this
		30 * time.Second:        "30s",
	} {
		if got := consulTTL(timeout); got != want {
			t.Fatalf("consulTTL(%s) = %q, want %q", timeout, got, want)
		}
	}
}

// TestConsulRejectsOutOfRangeTimeout pins the Consul-specific bound: the Consul
// server only accepts session TTLs between 10s and 86400s, so a lock timeout
// outside that range must be rejected at construction time.
func TestConsulRejectsOutOfRangeTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{
		9 * time.Second,
		consulMinSessionTTL - time.Millisecond,
		consulMaxSessionTTL + time.Second,
	} {
		if _, err := NewConsulLocker("127.0.0.1:8500", WithLockTimeout(timeout)); !errors.Is(err, ErrInvalidLockTimeout) {
			t.Fatalf("NewConsulLocker(WithLockTimeout(%s)) error = %v, want ErrInvalidLockTimeout", timeout, err)
		}
	}
	// The boundaries themselves are valid; this only needs to prove the
	// constructor got past validation, so a nil error is enough.
	for _, timeout := range []time.Duration{consulMinSessionTTL, consulMaxSessionTTL} {
		if _, err := NewConsulLocker("127.0.0.1:8500", WithLockTimeout(timeout)); err != nil {
			t.Fatalf("NewConsulLocker(WithLockTimeout(%s)) error = %v, want nil", timeout, err)
		}
	}
}

// TestConsulSessionUsesReleaseBehavior documents the session configuration: the
// key is released when the session expires, and the TTL matches the lock timeout.
func TestConsulSessionUsesReleaseBehavior(t *testing.T) {
	fake := newFakeConsul()
	l := newConsulLocker(fake, fake, ApplyOptions(
		WithLockName(consulTestKey),
		WithLockTimeout(30*time.Second),
	))
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	entry := fake.lastEntry()
	if entry == nil {
		t.Fatal("no session entry was created")
	}
	if entry.TTL != "30s" {
		t.Fatalf("session TTL = %q, want %q", entry.TTL, "30s")
	}
	if entry.Behavior != api.SessionBehaviorRelease {
		t.Fatalf("session behavior = %q, want %q", entry.Behavior, api.SessionBehaviorRelease)
	}
}

// TestConsulUnlockReleasesKeyAndDestroysSession asserts Unlock cleans up both the
// key and the session (the old implementation destroyed neither).
func TestConsulUnlockReleasesKeyAndDestroysSession(t *testing.T) {
	fake := newFakeConsul()
	l := newConsulLocker(fake, fake, ApplyOptions(WithLockName(consulTestKey), WithLockTimeout(10*time.Second)))
	ctx := context.Background()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	if err := l.Unlock(ctx); err != nil {
		t.Fatalf("Unlock() = %v, want nil", err)
	}
	if got := fake.tokenAt(consulTestKey); got != "" {
		t.Fatalf("key still present after Unlock(): %q", got)
	}
	if got := fake.liveSessions(); got != 0 {
		t.Fatalf("live sessions after Unlock() = %d, want 0", got)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}

// TestConsulStaleSessionCannotReleaseSuccessor is the ownership regression, made
// concrete for Consul's session model.
func TestConsulStaleSessionCannotReleaseSuccessor(t *testing.T) {
	fake := newFakeConsul()
	ctx := context.Background()
	a := newConsulLocker(fake, fake, ApplyOptions(WithLockName(consulTestKey), WithLockTimeout(10*time.Second)))
	b := newConsulLocker(fake, fake, ApplyOptions(WithLockName(consulTestKey), WithLockTimeout(10*time.Second)))

	if err := a.Lock(ctx); err != nil {
		t.Fatalf("a.Lock() = %v, want nil", err)
	}
	fake.expireSessions() // a stops renewing; Consul drops the session
	if err := b.Lock(ctx); err != nil {
		t.Fatalf("b.Lock() after a's session expired = %v, want nil", err)
	}
	tokenB := fake.tokenAt(consulTestKey)

	if err := a.Unlock(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("a.Unlock() = %v, want ErrLockNotHeld", err)
	}
	if got := fake.tokenAt(consulTestKey); got != tokenB {
		t.Fatalf("a.Unlock() destroyed b's key: %q, want %q", got, tokenB)
	}
	if err := b.Renew(ctx); err != nil {
		t.Fatalf("b.Renew() = %v, want nil", err)
	}
}
