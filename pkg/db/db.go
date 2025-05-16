package db

import (
	"github.com/google/wire"
	redis "github.com/redis/go-redis/v9"
)

// ProviderSet is db providers.
var ProviderSet = wire.NewSet(
	NewMySQL,
	NewRedis,
	wire.Bind(new(redis.UniversalClient), new(*redis.Client)), // 正确绑定接口和实现
)
