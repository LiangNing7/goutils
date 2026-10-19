package token

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyDefaultKey is the key that used to be hardcoded in this package. It is
// public knowledge and must never be usable as an implicit default again.
const legacyDefaultKey = "Rtg8BPKNEf2mB4mgvKONGPZZQSaJWNLijxR42qRgq0iBb5"

// resetConfig restores the package level configuration to its zero state.
func resetConfig() {
	config = Config{"", "identityKey", 2 * time.Hour}
}

// initialKey captures the key the package configures itself with before any
// test mutates it. Package level variables are initialized in dependency
// order, so it always sees the pristine default regardless of test ordering.
var initialKey = config.key

// TestPackageDefaultConfigHasNoKey asserts the package does not initialize
// itself with a usable hardcoded key. It used to be the public constant
// legacyDefaultKey, which let anybody forge a token.
func TestPackageDefaultConfigHasNoKey(t *testing.T) {
	assert.Empty(t, initialKey, "package must not ship an implicit default signing key")
}

// signWith signs the given claims with the given key.
func signWith(t *testing.T, key string, claims jwt.MapClaims) string {
	t.Helper()

	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	require.NoError(t, err)

	return tokenString
}

// TestNoImplicitDefaultKey asserts the package no longer ships a hardcoded
// signing key: without Init, signing and verifying both fail.
func TestNoImplicitDefaultKey(t *testing.T) {
	resetConfig()

	tokenString, _, err := Sign("alice")
	assert.Error(t, err, "Sign must fail when no key was configured")
	assert.Empty(t, tokenString)
}

// TestLegacyDefaultKeyTokenRejected asserts a token forged with the historical
// hardcoded key is rejected.
func TestLegacyDefaultKeyTokenRejected(t *testing.T) {
	resetConfig()

	forged := signWith(t, legacyDefaultKey, jwt.MapClaims{
		config.identityKey: "admin",
		"exp":              time.Now().Add(time.Hour).Unix(),
	})

	identity, err := Parse(forged, config.key)
	assert.Error(t, err, "token forged with the legacy default key must be rejected")
	assert.Empty(t, identity)
}

// TestParseRequestRejectsLegacyDefaultKey asserts the request helper does not
// accept a token forged with the historical hardcoded key either.
func TestParseRequestRejectsLegacyDefaultKey(t *testing.T) {
	resetConfig()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+signWith(t, legacyDefaultKey, jwt.MapClaims{
		config.identityKey: "admin",
		"exp":              time.Now().Add(time.Hour).Unix(),
	}))

	c, _ := gin.CreateTestContext(w)
	c.Request = req

	identity, err := ParseRequest(c)
	assert.Error(t, err, "request with a token forged with the legacy default key must be rejected")
	assert.Empty(t, identity)
}

// TestParseRejectsTokenWithoutExp asserts the exp claim is mandatory.
func TestParseRejectsTokenWithoutExp(t *testing.T) {
	resetConfig()
	Init("real-key", "", 0)

	noExp := signWith(t, "real-key", jwt.MapClaims{
		config.identityKey: "alice",
		"nbf":              time.Now().Unix(),
		"iat":              time.Now().Unix(),
	})

	identity, err := Parse(noExp, config.key)
	assert.Error(t, err, "token without an exp claim must be rejected")
	assert.Empty(t, identity)
}

// TestParseRejectsExpiredToken guards the expiry check.
func TestParseRejectsExpiredToken(t *testing.T) {
	resetConfig()
	Init("real-key", "", 0)

	expired := signWith(t, "real-key", jwt.MapClaims{
		config.identityKey: "alice",
		"exp":              time.Now().Add(-time.Hour).Unix(),
	})

	identity, err := Parse(expired, config.key)
	assert.Error(t, err)
	assert.Empty(t, identity)
}

// TestParseRejectsEmptyKey asserts an empty key is never used to verify.
func TestParseRejectsEmptyKey(t *testing.T) {
	resetConfig()
	Init("real-key", "", 0)

	valid := signWith(t, "real-key", jwt.MapClaims{
		config.identityKey: "alice",
		"exp":              time.Now().Add(time.Hour).Unix(),
	})

	identity, err := Parse(valid, "")
	assert.Error(t, err)
	assert.Empty(t, identity)
}

// TestSignAndParseRoundTrip is the positive control.
func TestSignAndParseRoundTrip(t *testing.T) {
	resetConfig()
	Init("real-key", "", 0)

	tokenString, expireAt, err := Sign("alice")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(2*time.Hour), expireAt, time.Minute)

	identity, err := Parse(tokenString, config.key)
	require.NoError(t, err)
	assert.Equal(t, "alice", identity)
}

// TestParseRequestWithGinBearerCaseInsensitive asserts the scheme in the
// Authorization header is matched case insensitively, as RFC 7235 requires.
func TestParseRequestWithGinBearerCaseInsensitive(t *testing.T) {
	resetConfig()
	Init("real-key", "", 0)

	tokenString, _, err := Sign("alice")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "bearer "+tokenString)

	c, _ := gin.CreateTestContext(w)
	c.Request = req

	identity, err := ParseRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "alice", identity)
}

// TestParseRequestWithGinMalformedHeader asserts malformed Authorization
// headers are reported as errors instead of silently yielding an empty token.
func TestParseRequestWithGinMalformedHeader(t *testing.T) {
	resetConfig()
	Init("real-key", "", 0)

	for _, header := range []string{"", "Bearer", "Bearer ", "bearer", "Token abc", "Bearer abc def", "  "} {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", header)

		c, _ := gin.CreateTestContext(w)
		c.Request = req

		identity, err := ParseRequest(c)
		assert.Error(t, err, "header %q must be rejected", header)
		assert.Empty(t, identity)
	}
}
