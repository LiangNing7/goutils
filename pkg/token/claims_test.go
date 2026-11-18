package token

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const claimsTestKey = "claims-test-signing-key"

func TestSignWithClaimsRoundTrip(t *testing.T) {
	resetConfig()
	Init(claimsTestKey, "identityKey", time.Hour)

	custom := jwt.MapClaims{
		"iss":     "nora-runtime",
		"aud":     "agent:agent-1",
		"actions": []string{"read", "prompt"},
	}
	signed, expireAt, err := SignWithClaims(custom)
	require.NoError(t, err)
	assert.False(t, expireAt.IsZero())

	claims, err := ParseWithKey(signed, claimsTestKey)
	require.NoError(t, err)
	assert.Equal(t, "nora-runtime", claims["iss"])
	assert.Equal(t, "agent:agent-1", claims["aud"])
	assert.Equal(t, []any{"read", "prompt"}, claims["actions"])
	assert.Equal(t, float64(expireAt.Unix()), claims["exp"])
	assert.Contains(t, claims, "nbf")
	assert.Contains(t, claims, "iat")
}

func TestSignWithClaimsKeepsProvidedTimestamps(t *testing.T) {
	resetConfig()
	Init(claimsTestKey, "identityKey", time.Hour)

	exp := time.Now().Add(10 * time.Minute).Unix()
	signed, expireAt, err := SignWithClaims(jwt.MapClaims{"sub": "user-1", "exp": exp})
	require.NoError(t, err)
	assert.Equal(t, exp, expireAt.Unix())

	claims, err := ParseWithKey(signed, claimsTestKey)
	require.NoError(t, err)
	assert.Equal(t, float64(exp), claims["exp"])
}

func TestSignWithClaimsDoesNotMutateInput(t *testing.T) {
	resetConfig()
	Init(claimsTestKey, "identityKey", time.Hour)

	custom := jwt.MapClaims{"sub": "user-1"}
	_, _, err := SignWithClaims(custom)
	require.NoError(t, err)
	assert.Len(t, custom, 1)
	assert.NotContains(t, custom, "exp")
}

func TestSignWithClaimsWithoutKey(t *testing.T) {
	resetConfig()

	_, _, err := SignWithClaims(jwt.MapClaims{"sub": "user-1"})
	assert.ErrorIs(t, err, ErrKeyNotInitialized)
}

func TestParseWithKeyRejects(t *testing.T) {
	resetConfig()
	Init(claimsTestKey, "identityKey", time.Hour)

	signed, _, err := SignWithClaims(jwt.MapClaims{"sub": "user-1"})
	require.NoError(t, err)

	missingExp, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "user-1"}).
		SignedString([]byte(claimsTestKey))
	require.NoError(t, err)

	tests := []struct {
		name  string
		token string
		key   string
	}{
		{name: "empty key", token: signed, key: ""},
		{name: "wrong key", token: signed, key: "another-key"},
		{name: "tampered", token: signed + "x", key: claimsTestKey},
		{name: "missing exp", token: missingExp, key: claimsTestKey},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseWithKey(test.token, test.key); err == nil {
				t.Fatal("invalid token was accepted")
			}
		})
	}
}
