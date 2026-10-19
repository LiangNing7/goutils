package options

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMongoOptions_Validate_URL verifies that Validate rejects empty and
// malformed MongoDB connection URLs while accepting valid ones.
//
// Before the fix `url.Parse` was used, which reports no error for "" and
// "not a url at all" and therefore silently allowed the driver to fall back to
// mongodb://localhost:27017.
func TestMongoOptions_Validate_URL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"empty", "", true},
		{"plain text", "not a url at all", true},
		{"host port without scheme", "127.0.0.1:27017", true},
		{"scheme only", "mongodb://", true},
		{"http scheme", "http://127.0.0.1:27017", true},
		{"valid ipv4", "mongodb://127.0.0.1:27017", false},
		{"valid with credentials", "mongodb://user:pass@127.0.0.1:27017/admin", false},
		{"valid hostname", "mongodb://mongo.internal:27017", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := NewMongoOptions()
			o.URL = c.url
			o.Database = "testdb"
			o.Collection = "testcoll"

			errs := o.Validate()
			if c.wantErr {
				require.NotEmpty(t, errs, "URL %q must be rejected", c.url)
			} else {
				require.Empty(t, errs, "URL %q must be accepted", c.url)
			}
		})
	}
}

// TestMongoOptions_NewClient_RejectsInvalidURL makes sure NewClient never
// silently connects to localhost when URL is empty.
func TestMongoOptions_NewClient_RejectsInvalidURL(t *testing.T) {
	for _, u := range []string{"", "not a url at all", "127.0.0.1:27017"} {
		o := NewMongoOptions()
		o.URL = u
		o.Database = "testdb"
		o.Collection = "testcoll"

		_, err := o.NewClient()
		require.Error(t, err, "NewClient must fail fast for URL %q", u)
	}
}

// TestMongoOptions_EffectiveTimeout verifies that a non-positive Timeout falls
// back to the driver default instead of producing an already-expired context.
func TestMongoOptions_EffectiveTimeout(t *testing.T) {
	o := NewMongoOptions()
	require.Equal(t, defaultMongoTimeout, o.effectiveTimeout())

	o.Timeout = 0
	require.Equal(t, defaultMongoTimeout, o.effectiveTimeout())

	o.Timeout = -5 * time.Second
	require.Equal(t, defaultMongoTimeout, o.effectiveTimeout())

	o.Timeout = 5 * time.Second
	require.Equal(t, 5*time.Second, o.effectiveTimeout())
}

// TestMongoOptions_ZeroTimeoutContextNotExpired reproduces the original bug:
// context.WithTimeout(ctx, 0) immediately expires, so Connect/Ping always fail.
func TestMongoOptions_ZeroTimeoutContextNotExpired(t *testing.T) {
	o := NewMongoOptions()
	o.Timeout = 0

	ctx, cancel := context.WithTimeout(context.Background(), o.effectiveTimeout())
	defer cancel()

	require.NoError(t, ctx.Err(), "a zero Timeout must not produce an expired context")

	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.Greater(t, time.Until(deadline), time.Second)
}
