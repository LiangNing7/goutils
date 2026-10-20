package jwt

import (
	"context"
	"maps"
	"time"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/golang-jwt/jwt/v4"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/LiangNing7/goutils/pkg/authn"
	"github.com/LiangNing7/goutils/pkg/i18n"
)

const (
	// reason holds the error reason.
	reason string = "Unauthorized"

	// reasonConfig holds the error reason for server side misconfiguration.
	reasonConfig string = "AuthConfigError"
)

var (
	ErrTokenInvalid           = errors.Unauthorized(reason, "Token is invalid")
	ErrTokenExpired           = errors.Unauthorized(reason, "Token has expired")
	ErrTokenParseFail         = errors.Unauthorized(reason, "Fail to parse token")
	ErrUnSupportSigningMethod = errors.Unauthorized(reason, "Wrong signing method")
	ErrSignTokenFailed        = errors.Unauthorized(reason, "Failed to sign token")
	// ErrTokenMissingExpiry is returned when a token does not carry the
	// required `exp` claim.
	ErrTokenMissingExpiry = errors.Unauthorized(reason, "Token is missing the required expiry claim")
	// ErrSigningKeyMissing is returned when neither a signing key nor a
	// keyfunc was configured. There is deliberately no implicit default key.
	ErrSigningKeyMissing = errors.InternalServer(reasonConfig, "Signing key is not configured")
)

// Define i18n messages.
var (
	MessageTokenInvalid           = &goi18n.Message{ID: "jwt.token.invalid", Other: ErrTokenInvalid.Error()}
	MessageTokenExpired           = &goi18n.Message{ID: "jwt.token.expired", Other: ErrTokenExpired.Error()}
	MessageTokenParseFail         = &goi18n.Message{ID: "jwt.token.parse.failed", Other: ErrTokenParseFail.Error()}
	MessageUnSupportSigningMethod = &goi18n.Message{ID: "jwt.wrong.signing.method", Other: ErrUnSupportSigningMethod.Error()}
	MessageSignTokenFailed        = &goi18n.Message{ID: "jwt.token.sign.failed", Other: ErrSignTokenFailed.Error()}
	MessageTokenMissingExpiry     = &goi18n.Message{ID: "jwt.token.missing.expiry", Other: ErrTokenMissingExpiry.Error()}
	MessageSigningKeyMissing      = &goi18n.Message{ID: "jwt.signing.key.missing", Other: ErrSigningKeyMissing.Error()}
)

var defaultOptions = options{
	tokenType:     "Bearer",
	expired:       2 * time.Hour,
	signingMethod: jwt.SigningMethodHS256,
}

type options struct {
	signingMethod jwt.SigningMethod
	signingKey    any
	keyfunc       jwt.Keyfunc
	issuer        string
	expired       time.Duration
	tokenType     string
	tokenHeader   map[string]any

	// signingKeySet records whether the caller explicitly configured a signing
	// key through WithSigningKey.
	signingKeySet bool
	// keyfuncSet records whether the caller explicitly configured a keyfunc
	// through WithKeyfunc.
	keyfuncSet bool
}

// Option is jwt option.
type Option func(*options)

// WithSigningMethod set signature method.
func WithSigningMethod(method jwt.SigningMethod) Option {
	return func(o *options) {
		o.signingMethod = method
	}
}

// WithIssuer set token issuer which is identifies the principal that issued the JWT.
func WithIssuer(issuer string) Option {
	return func(o *options) {
		o.issuer = issuer
	}
}

// WithSigningKey set the signature key.
func WithSigningKey(key any) Option {
	return func(o *options) {
		o.signingKey = key
		o.signingKeySet = true
	}
}

// WithKeyfunc set the callback function for verifying the key.
func WithKeyfunc(keyFunc jwt.Keyfunc) Option {
	return func(o *options) {
		o.keyfunc = keyFunc
		o.keyfuncSet = true
	}
}

// WithExpired set the token expiration time (in seconds, default 2h).
func WithExpired(expired time.Duration) Option {
	return func(o *options) {
		o.expired = expired
	}
}

// WithTokenHeader set the customer tokenHeader for client side.
func WithTokenHeader(header map[string]any) Option {
	return func(o *options) {
		o.tokenHeader = header
	}
}

// New create a authentication instance.
func New(store Storer, opts ...Option) *JWTAuth {
	o := defaultOptions
	for _, opt := range opts {
		opt(&o)
	}

	// When the caller did not provide its own keyfunc, derive it from the
	// resolved options so that signing and verification always use the very
	// same key. A package level keyfunc returning a hardcoded key would let
	// anybody forge tokens.
	if !o.keyfuncSet {
		key := o.signingKey
		o.keyfunc = func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, ErrTokenInvalid
			}
			if isSigningKeyMissing(key) {
				return nil, ErrSigningKeyMissing
			}
			return key, nil
		}
	}

	return &JWTAuth{opts: &o, store: store}
}

// isSigningKeyMissing reports whether the configured signing key is absent or
// empty. An implicit or empty key must never be used to sign or verify tokens.
func isSigningKeyMissing(key any) bool {
	switch k := key.(type) {
	case nil:
		return true
	case []byte:
		return len(k) == 0
	case string:
		return k == ""
	default:
		return false
	}
}

// JWTAuth implement the authn.Authenticator interface.
type JWTAuth struct {
	opts  *options
	store Storer
}

// Sign is used to generate a token.
func (a *JWTAuth) Sign(ctx context.Context, userID string) (authn.IToken, error) {
	// Refuse to sign with an implicit or empty key: the caller must configure
	// one explicitly through WithSigningKey.
	if isSigningKeyMissing(a.opts.signingKey) {
		return nil, errors.InternalServer(reasonConfig, i18n.FromContext(ctx).LocalizeT(MessageSigningKeyMissing))
	}

	now := time.Now()
	expiresAt := now.Add(a.opts.expired)

	token := jwt.NewWithClaims(a.opts.signingMethod, &jwt.RegisteredClaims{
		// Issuer = iss,令牌颁发者。它表示该令牌是由谁创建的
		Issuer: a.opts.issuer,
		// IssuedAt = iat,令牌颁发时的时间戳。它表示令牌是何时被创建的
		IssuedAt: jwt.NewNumericDate(now),
		// ExpiresAt = exp,令牌的过期时间戳。它表示令牌将在何时过期
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		// NotBefore = nbf,令牌的生效时的时间戳。它表示令牌从什么时候开始生效
		NotBefore: jwt.NewNumericDate(now),
		// Subject = sub,令牌的主体。它表示该令牌是关于谁的
		Subject: userID,
	})
	if a.opts.tokenHeader != nil {
		maps.Copy(token.Header, a.opts.tokenHeader)
	}

	refreshToken, err := token.SignedString(a.opts.signingKey)
	if err != nil {
		return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageSignTokenFailed))
	}

	tokenInfo := &tokenInfo{
		ExpiresAt: expiresAt.Unix(),
		Type:      a.opts.tokenType,
		Token:     refreshToken,
	}

	return tokenInfo, nil
}

// parseToken is used to parse the input refreshToken.
func (a *JWTAuth) parseToken(ctx context.Context, refreshToken string) (*jwt.RegisteredClaims, error) {
	if a.opts.keyfunc == nil {
		return nil, errors.InternalServer(reasonConfig, i18n.FromContext(ctx).LocalizeT(MessageSigningKeyMissing))
	}

	// Restricted to the configured signing method so that an attacker cannot
	// downgrade the algorithm (e.g. alg=none or a swapped key type).
	parser := jwt.NewParser(jwt.WithValidMethods([]string{a.opts.signingMethod.Alg()}))

	token, err := parser.ParseWithClaims(refreshToken, &jwt.RegisteredClaims{}, a.opts.keyfunc)
	if err != nil {
		ve, ok := err.(*jwt.ValidationError)
		if !ok {
			return nil, errors.Unauthorized(reason, err.Error())
		}
		if ve.Errors&jwt.ValidationErrorMalformed != 0 {
			return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenInvalid))
		}
		if ve.Errors&(jwt.ValidationErrorExpired|jwt.ValidationErrorNotValidYet) != 0 {
			return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenExpired))
		}
		return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenParseFail))
	}

	if !token.Valid {
		return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenInvalid))
	}

	if token.Method != a.opts.signingMethod {
		return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageUnSupportSigningMethod))
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenInvalid))
	}

	// jwt/v4 does not require `exp`, and an absent `exp` makes ExpiresAt nil,
	// which panics downstream (see Destroy). Require it explicitly.
	if claims.ExpiresAt == nil {
		return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenMissingExpiry))
	}

	return claims, nil
}

func (a *JWTAuth) callStore(fn func(Storer) error) error {
	if store := a.store; store != nil {
		return fn(store)
	}
	return nil
}

// Destroy is used to destroy a token.
func (a *JWTAuth) Destroy(ctx context.Context, refreshToken string) error {
	claims, err := a.parseToken(ctx, refreshToken)
	if err != nil {
		return err
	}

	// If storage is set, put the unexpired token in
	store := func(store Storer) error {
		if claims == nil || claims.ExpiresAt == nil {
			return errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenMissingExpiry))
		}

		expired := time.Until(claims.ExpiresAt.Time)
		return store.Set(ctx, refreshToken, expired)
	}
	return a.callStore(store)
}

// ParseClaims parse the token and return the claims.
func (a *JWTAuth) ParseClaims(ctx context.Context, refreshToken string) (*jwt.RegisteredClaims, error) {
	if refreshToken == "" {
		return nil, errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenInvalid))
	}

	claims, err := a.parseToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	store := func(store Storer) error {
		exists, err := store.Check(ctx, refreshToken)
		if err != nil {
			return err
		}

		if exists {
			return errors.Unauthorized(reason, i18n.FromContext(ctx).LocalizeT(MessageTokenInvalid))
		}

		return nil
	}

	if err := a.callStore(store); err != nil {
		return nil, err
	}

	return claims, nil
}

// Release used to release the requested resources.
func (a *JWTAuth) Release() error {
	return a.callStore(func(store Storer) error {
		return store.Close()
	})
}
