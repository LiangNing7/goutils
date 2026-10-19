package options

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// secretMarker is a deliberately distinctive value used to detect leaks.
const secretMarker = "SUPER_SECRET_MARKER_VALUE"

// secretOptions returns one instance per options type that carries a credential
// or a private key, pre-populated with the marker value.
func secretOptions() map[string]any {
	return map[string]any{
		"mysql":      &MySQLOptions{Password: secretMarker},
		"postgresql": &PostgreSQLOptions{Password: secretMarker},
		"redis":      &RedisOptions{Password: secretMarker},
		"mongo":      &MongoOptions{Password: secretMarker},
		"kafka":      &KafkaOptions{Password: secretMarker},
		"etcd":       &EtcdOptions{Password: secretMarker},
		"jwt":        &JWTOptions{Key: secretMarker},
		"tls":        &TLSOptions{Key: secretMarker},
	}
}

// TestSecretFieldsNotExportedViaJSON is the credential-leak regression guard:
// marshalling any options struct must never emit the password / private key.
func TestSecretFieldsNotExportedViaJSON(t *testing.T) {
	for name, o := range secretOptions() {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(o)
			require.NoError(t, err)

			out := string(b)
			require.NotContains(t, out, secretMarker, "%s must not leak its secret through json.Marshal", name)
			require.NotContains(t, strings.ToLower(out), "password", "%s must not export a password key", name)
		})
	}
}

// TestTLSOptions_KeyNotExported specifically covers the private key path which
// used to be serialised as `"key"`.
func TestTLSOptions_KeyNotExported(t *testing.T) {
	o := &TLSOptions{Cert: "/tmp/c.crt", Key: "/tmp/c.key"}

	b, err := json.Marshal(o)
	require.NoError(t, err)
	require.NotContains(t, string(b), "/tmp/c.key")
}

// TestDefaultsDoNotContainLeakedCredentials makes sure the hard-coded `onex`
// credentials were removed from the shipped defaults.
func TestDefaultsDoNotContainLeakedCredentials(t *testing.T) {
	// NOTE: SecureServingOptions intentionally excluded - its "onex" occurrence
	// is a cert file pair name / directory, not a credential.
	for name, o := range map[string]any{
		"mysql":      NewMySQLOptions(),
		"postgresql": NewPostgreSQLOptions(),
		"jwt":        NewJWTOptions(),
	} {
		b, err := json.Marshal(o)
		require.NoError(t, err)

		require.NotContains(t, string(b), "onex(#)666", "%s default password leaked", name)
		require.NotContains(t, string(b), "onex", "%s default config leaked `onex`", name)
	}
}

// TestMySQLOptions_DefaultPasswordIsEmpty documents the new (safe) default.
func TestMySQLOptions_DefaultPasswordIsEmpty(t *testing.T) {
	require.Empty(t, NewMySQLOptions().Password)
	require.Empty(t, NewPostgreSQLOptions().Password)
	require.NotEqual(t, "onex(#)666", NewJWTOptions().Key)
}
