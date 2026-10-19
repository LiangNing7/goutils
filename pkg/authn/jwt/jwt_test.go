package jwt

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// forgedDefaultKey is the historically hardcoded default signing key of this
// package. It must never be accepted for verification.
const forgedDefaultKey = "LiangNing666"

// memStore is an in-memory Storer used to exercise the store code paths.
type memStore struct {
	mu    sync.Mutex
	items map[string]time.Duration
}

func newMemStore() *memStore {
	return &memStore{items: make(map[string]time.Duration)}
}

func (s *memStore) Set(_ context.Context, accessToken string, expiration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[accessToken] = expiration
	return nil
}

func (s *memStore) Delete(_ context.Context, accessToken string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[accessToken]
	delete(s.items, accessToken)
	return ok, nil
}

func (s *memStore) Check(_ context.Context, accessToken string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[accessToken]
	return ok, nil
}

func (s *memStore) Close() error { return nil }

// signHS256 signs the given claims with the provided key.
func signHS256(t *testing.T, key string, claims jwt.Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatalf("sign token with key %q: %v", key, err)
	}
	return token
}

// validClaims returns claims that are valid right now.
func validClaims(subject string) *jwt.RegisteredClaims {
	now := time.Now()
	return &jwt.RegisteredClaims{
		Issuer:    "goutils",
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
}

// TestParseClaimsRejectsForgedDefaultKeyToken asserts a token forged with the
// historical hardcoded key is rejected even when a real signing key is set.
func TestParseClaimsRejectsForgedDefaultKeyToken(t *testing.T) {
	ctx := context.Background()
	a := New(nil, WithSigningKey([]byte("my-real-secret")))

	forged := signHS256(t, forgedDefaultKey, validClaims("admin"))

	claims, err := a.ParseClaims(ctx, forged)
	if err == nil {
		t.Fatalf("forged token signed with the default key was accepted, claims: %+v", claims)
	}
	if claims != nil {
		t.Fatalf("forged token returned claims: %+v", claims)
	}
}

// TestParseClaimsRejectsForgedDefaultKeyTokenWithDefaultOptions asserts the
// out-of-the-box configuration does not accept the historical hardcoded key.
func TestParseClaimsRejectsForgedDefaultKeyTokenWithDefaultOptions(t *testing.T) {
	ctx := context.Background()
	a := New(nil)

	forged := signHS256(t, forgedDefaultKey, validClaims("admin"))

	if _, err := a.ParseClaims(ctx, forged); err == nil {
		t.Fatal("forged token signed with the default key was accepted by the default configuration")
	}
}

// TestSignAndParseWithCustomKey asserts a token signed with a custom key is
// verifiable by the same instance.
func TestSignAndParseWithCustomKey(t *testing.T) {
	ctx := context.Background()
	a := New(nil, WithSigningKey([]byte("my-real-secret")))

	token, err := a.Sign(ctx, "user-1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	claims, err := a.ParseClaims(ctx, token.GetToken())
	if err != nil {
		t.Fatalf("ParseClaims: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}
}

// TestCrossKeyTokenRejected asserts tokens signed with a different key are
// rejected.
func TestCrossKeyTokenRejected(t *testing.T) {
	ctx := context.Background()
	signer := New(nil, WithSigningKey([]byte("key-a")))
	verifier := New(nil, WithSigningKey([]byte("key-b")))

	token, err := signer.Sign(ctx, "user-1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if _, err := verifier.ParseClaims(ctx, token.GetToken()); err == nil {
		t.Fatal("token signed with another key was accepted")
	}
}

// TestSignWithoutSigningKeyFails asserts Sign refuses to fall back to an
// implicit default key.
func TestSignWithoutSigningKeyFails(t *testing.T) {
	if _, err := New(nil).Sign(context.Background(), "user-1"); err == nil {
		t.Fatal("Sign succeeded without an explicitly configured signing key")
	}

	if _, err := New(nil, WithSigningKey("")).Sign(context.Background(), "user-1"); err == nil {
		t.Fatal("Sign succeeded with an empty signing key")
	}
}

// TestParseClaimsRequiresExpiry asserts a token without the exp claim is
// rejected, so downstream code can rely on ExpiresAt being present.
func TestParseClaimsRequiresExpiry(t *testing.T) {
	ctx := context.Background()
	a := New(nil, WithSigningKey([]byte("my-real-secret")))

	noExp := signHS256(t, "my-real-secret", &jwt.RegisteredClaims{Subject: "user-1"})

	if _, err := a.ParseClaims(ctx, noExp); err == nil {
		t.Fatal("token without an exp claim was accepted")
	}
}

// TestDestroyTokenWithoutExpiryReturnsError asserts Destroy does not panic on
// a token that has no exp claim. The token is forged with the historical
// default key, which is exactly the token that reached the nil ExpiresAt
// dereference before the fix.
func TestDestroyTokenWithoutExpiryReturnsError(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	a := New(store)

	noExp := signHS256(t, forgedDefaultKey, &jwt.RegisteredClaims{Subject: "user-1"})

	if err := a.Destroy(ctx, noExp); err == nil {
		t.Fatal("Destroy accepted a token without an exp claim")
	}
}

// TestDestroyTokenWithoutExpiryReturnsErrorWithCustomKey is the same
// assertion with an explicitly configured key: it exercises the defensive nil
// check inside Destroy even if the parse level check is relaxed later.
func TestDestroyTokenWithoutExpiryReturnsErrorWithCustomKey(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	a := New(store, WithSigningKey([]byte("my-real-secret")))

	noExp := signHS256(t, "my-real-secret", &jwt.RegisteredClaims{Subject: "user-1"})

	if err := a.Destroy(ctx, noExp); err == nil {
		t.Fatal("Destroy accepted a token without an exp claim")
	}
}

// TestDestroyValidTokenStoresIt asserts the happy path of Destroy still works.
func TestDestroyValidTokenStoresIt(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	a := New(store, WithSigningKey([]byte("my-real-secret")))

	token, err := a.Sign(ctx, "user-1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if err := a.Destroy(ctx, token.GetToken()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	// The destroyed token is now rejected by ParseClaims.
	if _, err := a.ParseClaims(ctx, token.GetToken()); err == nil {
		t.Fatal("destroyed token was still accepted")
	}
}

// TestCustomKeyfuncStillHonoured asserts WithKeyfunc keeps working and takes
// precedence over the signing key for verification.
func TestCustomKeyfuncStillHonoured(t *testing.T) {
	ctx := context.Background()
	called := false
	a := New(nil, WithSigningKey([]byte("ignored-by-verify")), WithKeyfunc(func(t *jwt.Token) (any, error) {
		called = true
		return []byte("custom-key"), nil
	}))

	token := signHS256(t, "custom-key", validClaims("user-2"))

	claims, err := a.ParseClaims(ctx, token)
	if err != nil {
		t.Fatalf("ParseClaims: %v", err)
	}
	if !called {
		t.Fatal("custom keyfunc was not called")
	}
	if claims.Subject != "user-2" {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}
}

// TestKeyfuncWithoutSigningKeyCannotSign documents that a keyfunc only
// configures verification: without an explicit signing key Sign refuses to
// run. This is the intended fail-closed behaviour, a caller that used to rely
// on the implicit default key for signing must now pass WithSigningKey.
func TestKeyfuncWithoutSigningKeyCannotSign(t *testing.T) {
	a := New(nil, WithKeyfunc(func(t *jwt.Token) (any, error) {
		return []byte("custom-key"), nil
	}))

	if _, err := a.Sign(context.Background(), "user-1"); err == nil {
		t.Fatal("Sign succeeded without an explicitly configured signing key")
	}

	// Verification still works through the custom keyfunc.
	token := signHS256(t, "custom-key", validClaims("user-2"))
	if _, err := a.ParseClaims(context.Background(), token); err != nil {
		t.Fatalf("ParseClaims: %v", err)
	}
}
