package onex

import (
	"context"
	"errors"
	"testing"

	"github.com/LiangNing7/goutils/pkg/store"
)

// 编译期断言：onexLogger 必须满足 store.Logger 接口。
// 该断言缺失时，接口不匹配的 logger 会静默编译通过但实际不可用。
var _ store.Logger = (*onexLogger)(nil)

// TestOnexLoggerImplementsStoreLogger 通过 store.Logger 接口使用 onexLogger。
func TestOnexLoggerImplementsStoreLogger(t *testing.T) {
	var l store.Logger = NewLogger()
	l.Error(context.Background(), errors.New("boom"), "something failed", "k", "v")
}

func TestNewLoggerNotNil(t *testing.T) {
	if NewLogger() == nil {
		t.Fatal("NewLogger() returned nil")
	}
}
