package options

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// TestKafkaOptions_ZeroValue_NoPanic reproduces the nil TLSOptions dereference
// that happens for objects built without NewKafkaOptions (viper decoding, struct
// literals).
func TestKafkaOptions_ZeroValue_NoPanic(t *testing.T) {
	o := &KafkaOptions{}

	require.NotPanics(t, func() { _ = o.Validate() })
	require.NotPanics(t, func() { _, _ = o.Dialer() })
	require.NotPanics(t, func() {
		fs := pflag.NewFlagSet("kafka", pflag.ContinueOnError)
		o.AddFlags(fs)
	})
}

func TestEtcdOptions_ZeroValue_NoPanic(t *testing.T) {
	o := &EtcdOptions{}

	require.NotPanics(t, func() { _ = o.Validate() })
	require.NotPanics(t, func() {
		fs := pflag.NewFlagSet("etcd", pflag.ContinueOnError)
		o.AddFlags(fs)
	})
}

func TestMongoOptions_ZeroValue_NoPanic(t *testing.T) {
	o := &MongoOptions{}

	require.NotPanics(t, func() { _ = o.Validate() })
	require.NotPanics(t, func() {
		fs := pflag.NewFlagSet("mongo", pflag.ContinueOnError)
		o.AddFlags(fs)
	})
}

// TestTLSOptions_ZeroValue_NoPanic keeps TLSOptions usable without UseTLS.
func TestTLSOptions_ZeroValue_NoPanic(t *testing.T) {
	o := &TLSOptions{}

	require.NotPanics(t, func() { _, _ = o.TLSConfig() })
	require.NotPanics(t, func() { _ = o.MustTLSConfig() })
	require.NotPanics(t, func() { _ = o.Validate() })
}

// TestTLSOptions_MustTLSConfig_ExposesError makes sure the silent-swallow path
// can be detected by callers that want the real cause.
func TestTLSOptions_MustTLSConfig_ExposesError(t *testing.T) {
	o := &TLSOptions{UseTLS: true, CaCert: "/definitely/not/here/ca.pem"}

	require.NotPanics(t, func() {
		cfg := o.MustTLSConfig()
		require.NotNil(t, cfg)
	})

	_, err := o.MustTLSConfigE()
	require.Error(t, err, "the error-aware variant must surface the real cause")

	_, err = o.TLSConfig()
	require.Error(t, err)
}
