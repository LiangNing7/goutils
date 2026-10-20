package empty_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LiangNing7/goutils/pkg/store"
	"github.com/LiangNing7/goutils/pkg/store/logger/empty"
)

// 编译期断言：empty.NewLogger() 必须满足 store.Logger 接口。
// 注意 empty 被 store 依赖，因此只能用外部测试包 empty_test 以避免 import cycle。
var _ store.Logger = empty.NewLogger()

func TestEmptyLoggerImplementsStoreLogger(t *testing.T) {
	var l store.Logger = empty.NewLogger()
	l.Error(context.Background(), errors.New("boom"), "msg", "k", "v")
}

func TestNewLoggerNotNil(t *testing.T) {
	if empty.NewLogger() == nil {
		t.Fatal("NewLogger() returned nil")
	}
}
