package log

import (
	"fmt"

	krtlog "github.com/go-kratos/kratos/v2/log"
)

type KratosLogger interface {
	// Log implements is used to github.com/go-kratos/kratos/v2/log.Logger interface.
	Log(level krtlog.Level, keyvals ...any) error
}

// defaultLoggerAdapter 是一个 kratos Logger，它在每次 Log 时才解析当前的全局
// logger. 这样在 log.Init 之前构造出来的 Kratos logger 不会一直持有旧的 logger.
type defaultLoggerAdapter struct{}

// NewKratosLogger 返回一个始终指向全局 logger 的 kratos Logger.
func NewKratosLogger() krtlog.Logger {
	return defaultLoggerAdapter{}
}

// Log 实现 github.com/go-kratos/kratos/v2/log.Logger.
func (defaultLoggerAdapter) Log(level krtlog.Level, keyvals ...any) error {
	return std.Load().Log(level, keyvals...)
}

func (l *zapLogger) Log(level krtlog.Level, keyvals ...any) error {
	keylen := len(keyvals)
	if keylen == 0 || keylen%2 != 0 {
		l.z.Warn(fmt.Sprint("Keyvalues must appear in pairs: ", keyvals))
		return nil
	}

	switch level {
	case krtlog.LevelDebug:
		l.z.Sugar().Debugw("", keyvals...)
	case krtlog.LevelInfo:
		l.z.Sugar().Infow("", keyvals...)
	case krtlog.LevelWarn:
		l.z.Sugar().Warnw("", keyvals...)
	case krtlog.LevelError:
		l.z.Sugar().Errorw("", keyvals...)
	case krtlog.LevelFatal:
		l.z.Sugar().Fatalw("", keyvals...)
	}

	return nil
}
