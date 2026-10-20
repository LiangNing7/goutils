package onex

import (
	"context"

	"github.com/LiangNing7/goutils/pkg/log"
	"github.com/LiangNing7/goutils/pkg/store"
)

// 编译期断言：onexLogger 必须实现 store.Logger，避免接口不匹配时静默不可用。
var _ store.Logger = (*onexLogger)(nil)

// onexLogger is a logger that implements the Logger interface.
// It uses the log package to log error messages with additional context.
type onexLogger struct{}

// NewLogger creates and returns a new instance of onexLogger.
func NewLogger() *onexLogger {
	return &onexLogger{}
}

// Error logs an error message with the provided context using the log package.
func (l *onexLogger) Error(ctx context.Context, err error, msg string, kvs ...any) {
	log.Errorw(err, msg, kvs...)
}
