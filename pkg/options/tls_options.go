package options

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/spf13/pflag"

	"github.com/LiangNing7/goutils/pkg/log"
)

var _ IOptions = (*TLSOptions)(nil)

// TLSOptions is the TLS cert info for serving secure traffic.
type TLSOptions struct {
	// UseTLS specifies whether should be encrypted with TLS if possible.
	UseTLS             bool   `json:"use-tls" mapstructure:"use-tls"`
	InsecureSkipVerify bool   `json:"insecure-skip-verify" mapstructure:"insecure-skip-verify"`
	CaCert             string `json:"ca-cert" mapstructure:"ca-cert"`
	Cert               string `json:"cert" mapstructure:"cert"`
	// Key is the path to the private key file. It is never exported: it used to
	// be serialised as `"key"` by json.Marshal / structured logging.
	Key string `json:"-" mapstructure:"key"`
}

// NewTLSOptions create a `zero` value instance.
func NewTLSOptions() *TLSOptions {
	return &TLSOptions{}
}

// Validate verifies flags passed to TLSOptions.
func (o *TLSOptions) Validate() []error {
	errs := []error{}

	if !o.UseTLS {
		return errs
	}

	if (o.Cert != "" && o.Key == "") || (o.Cert == "" && o.Key != "") {
		errs = append(errs, fmt.Errorf("only one of cert and key configuration option is setted, you should set both to enable tls"))
	}

	return errs
}

// AddFlags adds flags related to TLS for a specific APIServer to the specified FlagSet.
func (o *TLSOptions) AddFlags(fs *pflag.FlagSet, prefixes ...string) {
	fs.BoolVar(&o.UseTLS, join(prefixes...)+"tls.use-tls", o.UseTLS, "Use tls transport to connect the server.")
	fs.BoolVar(&o.InsecureSkipVerify, join(prefixes...)+"tls.insecure-skip-verify", o.InsecureSkipVerify, ""+
		"Controls whether a client verifies the server's certificate chain and host name.")
	fs.StringVar(&o.CaCert, join(prefixes...)+"tls.ca-cert", o.CaCert, "Path to ca cert for connecting to the server.")
	fs.StringVar(&o.Cert, join(prefixes...)+"tls.cert", o.Cert, "Path to cert file for connecting to the server.")
	fs.StringVar(&o.Key, join(prefixes...)+"tls.key", o.Key, "Path to key file for connecting to the server.")
}

// MustTLSConfig returns a *tls.Config and never fails.
//
// Deprecated-ish behaviour note: a broken certificate configuration used to be
// swallowed silently, which surfaced much later as an opaque
// ListenAndServeTLS("", "")/handshake failure. The error is now reported through
// the logger, and callers that can handle it should use TLSConfig or
// MustTLSConfigE instead. The signature is kept for backwards compatibility.
func (o *TLSOptions) MustTLSConfig() *tls.Config {
	tlsConf, err := o.TLSConfig()
	if err != nil {
		log.Errorw(err, "failed to build tls config, continuing with an empty one")

		return &tls.Config{}
	}

	return tlsConf
}

// MustTLSConfigE is the error-aware variant of MustTLSConfig. It returns the same
// values MustTLSConfig would, but lets the caller see the real cause (for
// example a missing CA file).
func (o *TLSOptions) MustTLSConfigE() (*tls.Config, error) {
	tlsConf, err := o.TLSConfig()
	if err != nil {
		return &tls.Config{}, err
	}

	return tlsConf, nil
}

func (o *TLSOptions) TLSConfig() (*tls.Config, error) {
	if !o.UseTLS {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: o.InsecureSkipVerify,
	}

	if o.Cert != "" && o.Key != "" {
		var cert tls.Certificate
		cert, err := tls.LoadX509KeyPair(o.Cert, o.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to loading tls certificates: %w", err)
		}

		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if o.CaCert != "" {
		data, err := os.ReadFile(o.CaCert)
		if err != nil {
			return nil, err
		}

		capool := x509.NewCertPool()
		for {
			var block *pem.Block
			block, _ = pem.Decode(data)
			if block == nil {
				break
			}
			cacert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			capool.AddCert(cacert)
		}

		tlsConfig.RootCAs = capool
	}

	return tlsConfig, nil
}

// Scheme returns the URL scheme based on the TLS configuration.
func (o *TLSOptions) Scheme() string {
	if o.UseTLS {
		return "https"
	}
	return "http"
}
