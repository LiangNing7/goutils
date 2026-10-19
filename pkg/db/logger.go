package db

import (
	stdlog "log"
	"os"
	"time"

	"gorm.io/gorm/logger"
)

// gormSlowThreshold 是 gorm 记录慢 SQL 的阈值，与 gorm 默认值保持一致.
const gormSlowThreshold = 200 * time.Millisecond

// newGormLogger 构造一个默认的 gorm logger.
//
// 与 gorm 的 logger.Default 不同，这里默认启用 ParameterizedQueries：
// gorm 在 Trace 时会保留 SQL 中的占位符，不再调用 Explain 把参数插值进日志，
// 从而避免密码哈希、手机号、身份证号等 PII 被写入 stdout / 日志采集系统。
// 需要打印完整 SQL（含参数）时，调用方可以通过 MySQLOptions.Logger /
// PostgreSQLOptions.Logger 自行覆盖（自行注入的 logger 由调用方自担风险）.
func newGormLogger(w logger.Writer, level logger.LogLevel) logger.Interface {
	return logger.New(w, logger.Config{
		SlowThreshold:             gormSlowThreshold,
		LogLevel:                  level,
		IgnoreRecordNotFoundError: false,
		Colorful:                  false,
		ParameterizedQueries:      true,
	})
}

// defaultGormLogger 返回默认的 gorm logger：参数化 SQL、Warn 级别、输出到 stdout.
func defaultGormLogger() logger.Interface {
	return newGormLogger(stdlog.New(os.Stdout, "\r\n", stdlog.LstdFlags), logger.Warn)
}
