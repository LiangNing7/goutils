package distlock

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakeRedis is an in-memory stand-in for the handful of Redis commands
// RedisLocker uses: SET NX, GET and the two Lua scripts.
//
// The Lua scripts are emulated by matching against the script constants and
// reproducing their documented behaviour (`if get(key) == token then ... end`).
// TestRedisScriptsStillCompareTheToken guards the constants themselves, so a
// script that stopped comparing tokens would fail the suite.
type fakeRedis struct {
	mu     sync.Mutex
	values map[string]string
	expiry map[string]time.Time
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{
		values: make(map[string]string),
		expiry: make(map[string]time.Time),
	}
}

// expireKey makes the key disappear, as Redis would once its TTL elapsed.
func (f *fakeRedis) expireKey(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.values, key)
	delete(f.expiry, key)
}

func (f *fakeRedis) value(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reapLocked(key)
	return f.values[key]
}

func (f *fakeRedis) expiresAt(key string) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.expiry[key]
}

func (f *fakeRedis) reapLocked(key string) {
	if exp, ok := f.expiry[key]; ok && !exp.IsZero() && time.Now().After(exp) {
		delete(f.values, key)
		delete(f.expiry, key)
	}
}

func (f *fakeRedis) SetNX(_ context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reapLocked(key)
	if _, exists := f.values[key]; exists {
		return redis.NewBoolResult(false, nil)
	}
	str, ok := value.(string)
	if !ok {
		return redis.NewBoolResult(false, errFakeRedis)
	}
	f.values[key] = str
	f.expiry[key] = time.Now().Add(expiration)
	return redis.NewBoolResult(true, nil)
}

func (f *fakeRedis) Get(_ context.Context, key string) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reapLocked(key)
	value, exists := f.values[key]
	if !exists {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func (f *fakeRedis) Eval(_ context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	if len(keys) != 1 || len(args) < 1 {
		return redis.NewCmdResult(int64(0), errFakeRedis)
	}
	key := keys[0]
	token, _ := args[0].(string)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.reapLocked(key)

	switch script {
	case redisUnlockScript:
		if f.values[key] != token {
			return redis.NewCmdResult(int64(0), nil)
		}
		delete(f.values, key)
		delete(f.expiry, key)
		return redis.NewCmdResult(int64(1), nil)
	case redisRenewScript:
		if f.values[key] != token {
			return redis.NewCmdResult(int64(0), nil)
		}
		ttlMillis, _ := args[1].(string)
		millis, err := time.ParseDuration(ttlMillis + "ms")
		if err != nil {
			return redis.NewCmdResult(int64(0), err)
		}
		f.expiry[key] = time.Now().Add(millis)
		return redis.NewCmdResult(int64(1), nil)
	default:
		return redis.NewCmdResult(int64(0), errFakeRedis)
	}
}

// errFakeRedis is returned by the fake for inputs it does not understand, so a
// test that silently stops exercising the Lua path fails loudly.
var errFakeRedis = errors.New("fake redis: unsupported command")

const redisTestKey = "unit-test-lock"

func redisHarness(t *testing.T) *lockerHarness {
	t.Helper()
	fake := newFakeRedis()
	return &lockerHarness{
		newLocker: func() testLocker {
			return NewRedisLocker(fake, WithLockName(redisTestKey), WithLockTimeout(10*time.Second))
		},
		expireLease: func() { fake.expireKey(redisTestKey) },
		shared:      true,
	}
}

// TestRedisScriptsStillCompareTheToken guards the two Lua scripts: they must
// compare the stored value against the argument token before mutating anything.
// Without that comparison the token model would silently degrade back to an
// unconditional DEL/EXPIRE.
func TestRedisScriptsStillCompareTheToken(t *testing.T) {
	for name, script := range map[string]string{
		"unlock": redisUnlockScript,
		"renew":  redisRenewScript,
	} {
		if !strings.Contains(script, "redis.call('get', KEYS[1])") {
			t.Fatalf("%s script does not read the lock: %q", name, script)
		}
		if !strings.Contains(script, "== ARGV[1]") {
			t.Fatalf("%s script does not compare against the token: %q", name, script)
		}
	}
	if !strings.Contains(redisUnlockScript, "redis.call('del', KEYS[1])") {
		t.Fatalf("unlock script no longer deletes the key: %q", redisUnlockScript)
	}
	if !strings.Contains(redisRenewScript, "redis.call('pexpire', KEYS[1], ARGV[2])") {
		t.Fatalf("renew script no longer extends the TTL: %q", redisRenewScript)
	}
}

// TestRedisLockStoresAFreshTokenPerAcquisition proves the token really changes
// between acquisitions by looking at what Redis ends up holding.
func TestRedisLockStoresAFreshTokenPerAcquisition(t *testing.T) {
	fake := newFakeRedis()
	l := NewRedisLocker(fake, WithLockName(redisTestKey), WithLockTimeout(10*time.Second))
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		if err := l.Lock(ctx); err != nil {
			t.Fatalf("Lock() = %v, want nil", err)
		}
		stored := fake.value(redisTestKey)
		if stored == "" {
			t.Fatal("no token stored in redis after Lock()")
		}
		if seen[stored] {
			t.Fatalf("token %q was reused across acquisitions", stored)
		}
		seen[stored] = true
		if err := l.Unlock(ctx); err != nil {
			t.Fatalf("Unlock() = %v, want nil", err)
		}
		if got := fake.value(redisTestKey); got != "" {
			t.Fatalf("key still present after Unlock(): %q", got)
		}
	}
}

// TestRedisUnlockIsNoopAfterTakeover checks the exact regression: a stale holder
// must not delete the successor's key.
func TestRedisUnlockIsNoopAfterTakeover(t *testing.T) {
	fake := newFakeRedis()
	ctx := context.Background()
	a := NewRedisLocker(fake, WithLockName(redisTestKey), WithLockTimeout(10*time.Second))
	b := NewRedisLocker(fake, WithLockName(redisTestKey), WithLockTimeout(10*time.Second))

	if err := a.Lock(ctx); err != nil {
		t.Fatalf("a.Lock() = %v, want nil", err)
	}
	fake.expireKey(redisTestKey) // a's lease elapses
	if err := b.Lock(ctx); err != nil {
		t.Fatalf("b.Lock() = %v, want nil", err)
	}
	bToken := fake.value(redisTestKey)

	staleErr := a.Unlock(ctx)
	if !isErrLockNotHeld(staleErr) {
		t.Fatalf("a.Unlock() = %v, want ErrLockNotHeld", staleErr)
	}
	if got := fake.value(redisTestKey); got != bToken {
		t.Fatalf("b's lock was clobbered: key=%q, want %q", got, bToken)
	}
}

// TestRedisRenewExtendsTheTtl proves Renew really pushes the TTL forward.
func TestRedisRenewExtendsTheTtl(t *testing.T) {
	fake := newFakeRedis()
	l := NewRedisLocker(fake, WithLockName(redisTestKey), WithLockTimeout(10*time.Second))
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	// Pretend five seconds of the lease already elapsed.
	fake.mu.Lock()
	fake.expiry[redisTestKey] = time.Now().Add(5 * time.Second)
	fake.mu.Unlock()

	before := fake.expiresAt(redisTestKey)
	if err := l.Renew(ctx); err != nil {
		t.Fatalf("Renew() = %v, want nil", err)
	}
	after := fake.expiresAt(redisTestKey)
	if !after.After(before) {
		t.Fatalf("Renew() did not extend the TTL: before=%s after=%s", before, after)
	}
}

// TestRedisForeignHolderIsReportedNotAdopted pins the "fake success" bug: a
// SetNX that fails must never be reported as a successful acquisition, even when
// the stored value is readable.
func TestRedisForeignHolderIsReportedNotAdopted(t *testing.T) {
	fake := newFakeRedis()
	ctx := context.Background()
	l := NewRedisLocker(fake, WithLockName(redisTestKey), WithLockTimeout(10*time.Second))

	// Somebody else's lock, with the same ownerID the locker would use.
	if _, err := fake.SetNX(ctx, redisTestKey, "someone-else", 10*time.Second).Result(); err != nil {
		t.Fatalf("seeding redis: %v", err)
	}

	if err := l.Lock(ctx); !isErrLockAlreadyHeld(err) {
		t.Fatalf("Lock() = %v, want ErrLockAlreadyHeld", err)
	}
	if got := fake.value(redisTestKey); got != "someone-else" {
		t.Fatalf("Lock() overwrote a foreign lock: %q", got)
	}
}
