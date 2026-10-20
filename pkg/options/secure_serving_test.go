package options

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestSecureServingOptions_Validate_BindPort covers the operator-precedence bug
// on `s.Required && s.BindPort < 1 || s.BindPort > 65535`: with Required=false an
// out-of-range port produced the misleading "cannot be turned off" message.
func TestSecureServingOptions_Validate_BindPort(t *testing.T) {
	cases := []struct {
		name         string
		required     bool
		port         int
		wantErrs     int
		wantContains string
	}{
		{name: "not required, port 0 allowed", required: false, port: 0, wantErrs: 0},
		{name: "not required, port too big", required: false, port: 70000, wantErrs: 1, wantContains: "between 0 and 65535"},
		{name: "not required, port negative", required: false, port: -1, wantErrs: 1, wantContains: "between 0 and 65535"},
		{name: "required, port 0 rejected", required: true, port: 0, wantErrs: 1, wantContains: "cannot be turned off"},
		{name: "required, port too big", required: true, port: 70000, wantErrs: 1, wantContains: "between 1 and 65535"},
		{name: "required, valid port", required: true, port: 8443, wantErrs: 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := &SecureServingOptions{BindPort: c.port, Required: c.required}
			errs := o.Validate()
			require.Len(t, errs, c.wantErrs)

			if c.wantContains != "" {
				require.Contains(t, errs[0].Error(), c.wantContains)
			}
		})
	}
}

// TestSecureServingOptions_MapstructureTags makes sure `bind-address` and
// friends survive a viper/mapstructure decode (YAML config).
func TestSecureServingOptions_MapstructureTags(t *testing.T) {
	const cfg = `
bind-address: 1.2.3.4
bind-port: 9443
required: false
tls:
  cert-dir: /tmp/certs
  pair-name: myapp
  cert-key:
    cert-file: /tmp/certs/a.crt
    private-key-file: /tmp/certs/a.key
`

	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(strings.NewReader(cfg)))

	var o SecureServingOptions
	require.NoError(t, v.Unmarshal(&o))

	require.Equal(t, "1.2.3.4", o.BindAddress)
	require.Equal(t, 9443, o.BindPort)
	require.False(t, o.Required)
	require.Equal(t, "/tmp/certs", o.ServerCert.CertDirectory)
	require.Equal(t, "myapp", o.ServerCert.PairName)
	require.Equal(t, "/tmp/certs/a.crt", o.ServerCert.CertKey.CertFile)
	require.Equal(t, "/tmp/certs/a.key", o.ServerCert.CertKey.KeyFile)
}
