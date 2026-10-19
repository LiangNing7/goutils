// Package db provide useful functions to create mysql instance.
//
// 该包封装了 MySQL / PostgreSQL / Redis 的初始化逻辑：
//
//   - NewMySQL / NewPostgreSQL：根据 Options 创建 *gorm.DB，并设置连接池参数；
//   - NewRedis：创建 *redis.Client，Ping 失败时会关闭客户端并返回错误，避免连接池泄漏；
//   - TracePlugin：可选的 gorm 插件，用于记录 SQL 耗时（需调用方显式 db.Use(&db.TracePlugin{}) 注册），
//     Initialize 中所有回调注册的错误都会通过返回值暴露，不再被静默吞掉。
//
// 关于日志与 PII：
//   - MySQLOptions.Logger / PostgreSQLOptions.Logger 未设置时，默认使用参数化的 logger
//     （ParameterizedQueries = true）。GORM 的 Trace 会调用 Explain 把参数插值进 SQL，
//     若直接使用 gorm 的 logger.Default，密码哈希、手机号等 PII 会被写入 stdout 与日志采集系统。
//     默认值已改为保留占位符的参数化 SQL；如确需打印完整 SQL，请自行注入 Logger 并自行承担风险。
//   - MySQLOptions.DisableDefaultTransaction / PostgreSQLOptions.DisableDefaultTransaction
//     用于关闭 GORM 默认的写事务包装（SkipDefaultTransaction），默认关闭该开关（即保持 GORM 默认语义），
//     仅在明确接受"单条写语句在非事务上下文中执行"时才建议开启。
package db // import "github.com/LiangNing7/goutils/pkg/db"
