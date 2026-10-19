package token

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Config 包括 token 包的配置选项.
type Config struct {
	// key 用于签发和解析 token 的密钥.
	key string
	// identityKey 是 token 中用户身份的键.
	identityKey string
	// expiration 是签发的 token 过期时间
	expiration time.Duration
}

var (
	// ErrKeyNotInitialized 表示还没有通过 Init 注入密钥.
	// 本包不再内置默认密钥：历史默认密钥是公开常量，任何人都能据此伪造 token.
	ErrKeyNotInitialized = errors.New("token: signing key is not initialized, call Init first")
	// ErrMissingExpiration 表示 token 缺少必须的 exp 声明.
	ErrMissingExpiration = errors.New("token: token is missing the required exp claim")

	configMu sync.RWMutex
	config   = Config{"", "identityKey", 2 * time.Hour}
)

// Init 设置包级别的配置 config, config 会用于本包后面的 token 签发和解析.
// 每次调用都会生效，后调用覆盖先调用（历史实现用 sync.Once 使第二次调用被静默忽略，
// 导致配置在运行期无法更新且失败难以察觉）.
func Init(key string, identityKey string, expiration time.Duration) {
	configMu.Lock()
	defer configMu.Unlock()
	if key != "" {
		config.key = key // 设置密钥
	}
	if identityKey != "" {
		config.identityKey = identityKey // 设置身份键
	}
	if expiration != 0 {
		config.expiration = expiration
	}
}

func configSnapshot() Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return config
}

// Parse 使用指定的密钥 key 解析 token，解析成功返回 token 上下文，否则报错.
func Parse(tokenString string, key string) (string, error) {
	return parse(tokenString, key, configSnapshot().identityKey)
}

func parse(tokenString, key, identityClaim string) (string, error) {
	// 没有密钥时不使用空密钥去验签，直接报错.
	if key == "" {
		return "", ErrKeyNotInitialized
	}

	// 解析 token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		// 确保 token 加密算法是预期的加密算法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}

		return []byte(key), nil // 返回密钥
	})
	// 解析失败
	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", jwt.ErrSignatureInvalid
	}

	// jwt/v4 只校验「存在且已过期」的 exp，缺失 exp 时不会报错，
	// 因此这里显式要求 exp 存在.
	if _, exists := claims["exp"]; !exists {
		return "", ErrMissingExpiration
	}

	// 从 token 中取出 token 的主题
	identityKey, valid := claims[identityClaim].(string)
	if !valid || identityKey == "" {
		return "", jwt.ErrSignatureInvalid
	}

	return identityKey, nil
}

// ParseRequest 从请求头中获取令牌，并将其传递给 Parse 函数以解析令牌.
func ParseRequest(ctx context.Context) (string, error) {
	var (
		token string
		err   error
	)

	switch typed := ctx.(type) {
	// 使用 Gin 框架开发的 HTTP 服务
	case *gin.Context:
		header := typed.Request.Header.Get("Authorization")
		if len(header) == 0 {
			//nolint: err113
			return "", errors.New("the length of the `Authorization` header is zero") // 返回错误
		}

		// 从请求头中取出 token.
		// 认证方案大小写不敏感（RFC 7235），且格式必须严格为 `Bearer <token>`；
		// 历史实现用 Sscanf 且忽略错误，header 不合法时会静默得到空 token.
		scheme, value, found := strings.Cut(header, " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(value) == "" {
			//nolint: err113
			return "", fmt.Errorf("invalid `Authorization` header: expected `Bearer <token>`, got %q", header)
		}
		token = strings.TrimSpace(value)
	// 使用 google.golang.org/grpc 框架开发的 gRPC 服务
	default:
		token, err = auth.AuthFromMD(typed, "Bearer")
		if err != nil {
			return "", status.Errorf(codes.Unauthenticated, "invalid auth token")
		}
	}

	cfg := configSnapshot()
	return parse(token, cfg.key, cfg.identityKey) // 解析 token
}

// Sign 使用 jwtSecret 签发 token，token 的 claims 中会存放传入的 subject.
func Sign(identityKey string) (string, time.Time, error) {
	config := configSnapshot()
	// 未注入密钥时不允许签发 token.
	if config.key == "" {
		return "", time.Time{}, ErrKeyNotInitialized
	}

	// 计算过期时间
	expireAt := time.Now().Add(config.expiration)

	// Token 的内容
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		config.identityKey: identityKey,       // 存放用户身份
		"nbf":              time.Now().Unix(), // token 生效时间
		"iat":              time.Now().Unix(), // token 签发时间
		"exp":              expireAt.Unix(),   // token 过期时间
	})

	// 签发 token
	tokenString, err := token.SignedString([]byte(config.key))
	if err != nil {
		return "", time.Time{}, err
	}

	return tokenString, expireAt, nil // 返回 token 字符串、过期时间和错误
}
