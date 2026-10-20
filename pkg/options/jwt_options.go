package options

import (
	"fmt"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/spf13/pflag"
)

var _ IOptions = (*JWTOptions)(nil)

// JWTOptions contains configuration items related to API server features.
type JWTOptions struct {
	// Key is the private signing key. It is never exported via json.Marshal so
	// that it cannot end up in structured logs.
	Key           string        `json:"-" mapstructure:"key"`
	Expired       time.Duration `json:"expired" mapstructure:"expired"`
	MaxRefresh    time.Duration `json:"max-refresh" mapstructure:"max-refresh"`
	SigningMethod string        `json:"signing-method" mapstructure:"signing-method"`
}

// NewJWTOptions creates a JWTOptions object with default parameters.
//
// Key is intentionally empty: no default signing key is shipped, it MUST be
// provided by the configuration file, a command line flag or an environment
// variable. Validate therefore fails until it is set.
func NewJWTOptions() *JWTOptions {
	return &JWTOptions{
		// Realm:         "",
		Key:           "",
		Expired:       2 * time.Hour,
		MaxRefresh:    2 * time.Hour,
		SigningMethod: "HS512",
	}
}

// Validate is used to parse and validate the parameters entered by the user at
// the command line when the program starts.
func (s *JWTOptions) Validate() []error {
	var errs []error

	if !govalidator.StringLength(s.Key, "6", "32") {
		errs = append(errs, fmt.Errorf("--jwt.key must larger than 5 and little than 33"))
	}

	return errs
}

// AddFlags adds flags related to JWT tokens for a specific api server to the
// specified FlagSet.
func (s *JWTOptions) AddFlags(fs *pflag.FlagSet, prefixes ...string) {
	if fs == nil {
		return
	}

	// fs.StringVar(&s.Realm, "jwt.realm", s.Realm, "Realm name to display to the user.")
	fs.StringVar(&s.Key, "jwt.key", s.Key, "Private key used to sign jwt token.")
	fs.DurationVar(&s.Expired, "jwt.expired", s.Expired, "JWT token expiration time.")
	fs.DurationVar(&s.MaxRefresh, "jwt.max-refresh", s.MaxRefresh, ""+
		"This field allows clients to refresh their token until MaxRefresh has passed.")
	fs.StringVar(&s.SigningMethod, "jwt.signing-method", s.SigningMethod, "JWT token signature method.")
}
