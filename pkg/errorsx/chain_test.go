package errorsx_test

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/status"

	"github.com/LiangNing7/goutils/pkg/errorsx"
)

// customErr 用于验证错误链是否被保留.
type customErr struct {
	msg string
}

func (e *customErr) Error() string { return e.msg }

// TestErrorX_MutatorsReturnCopies 覆盖 WithMessage/WithMetadata/KV 应返回副本而不是修改接收者.
func TestErrorX_MutatorsReturnCopies(t *testing.T) {
	base := errorsx.New(400, "BadRequest", "original")

	withMsg := base.WithMessage("changed %d", 1)
	assert.NotSame(t, base, withMsg, "WithMessage should return a copy")
	assert.Equal(t, "original", base.Message, "receiver must not be mutated")
	assert.Equal(t, "changed 1", withMsg.Message)

	withMd := base.WithMetadata(map[string]string{"a": "1"})
	assert.NotSame(t, base, withMd, "WithMetadata should return a copy")
	assert.Nil(t, base.Metadata, "receiver metadata must not be mutated")
	assert.Equal(t, "1", withMd.Metadata["a"])

	withKV := base.KV("k", "v")
	assert.NotSame(t, base, withKV, "KV should return a copy")
	assert.Nil(t, base.Metadata, "receiver metadata must not be mutated")
	assert.Equal(t, "v", withKV.Metadata["k"])

	// 副本之间不共享 Metadata map
	assert.Nil(t, withMsg.Metadata)
	other := withMd.KV("b", "2")
	assert.NotContains(t, withMd.Metadata, "b", "KV must not mutate the source metadata map")
	assert.Equal(t, "2", other.Metadata["b"])
	assert.Equal(t, "1", other.Metadata["a"])

	// Code / Reason / Message 等字段在副本中保持一致
	assert.Equal(t, base.Code, withMsg.Code)
	assert.Equal(t, base.Reason, withMsg.Reason)
	assert.Equal(t, base.Code, withMd.Code)
}

// TestErrorX_WithRequestID 覆盖链式调用与 WithRequestID.
func TestErrorX_WithRequestID(t *testing.T) {
	base := errorsx.New(500, "Internal", "base")

	chained := base.WithMessage("msg").WithRequestID("req-1")
	assert.Equal(t, "msg", chained.Message)
	assert.Equal(t, "req-1", chained.Metadata["X-Request-ID"])
	assert.Equal(t, "base", base.Message)
	assert.Nil(t, base.Metadata)
}

// TestErrorX_SingletonNotPolluted 覆盖包级预定义错误被链式调用污染的问题.
func TestErrorX_SingletonNotPolluted(t *testing.T) {
	defer func() {
		// 断言结束时不残留任何污染
		assert.Equal(t, "Resource not found.", errorsx.ErrNotFound.Message)
		assert.Nil(t, errorsx.ErrNotFound.Metadata)
	}()

	derived := errorsx.FromError(errorsx.ErrNotFound).WithMessage("被改写了")
	assert.Equal(t, "被改写了", derived.Message)
	assert.Equal(t, "Resource not found.", errorsx.ErrNotFound.Message,
		"package level singleton must not be mutated")

	derivedKV := errorsx.ErrNotFound.KV("k", "v").WithRequestID("req-1")
	assert.Equal(t, "v", derivedKV.Metadata["k"])
	assert.Nil(t, errorsx.ErrNotFound.Metadata,
		"package level singleton metadata must not be mutated")

	// 既有语义：Code / Reason 仍可匹配
	assert.True(t, errorsx.Is(derived, errorsx.ErrNotFound))
	assert.Equal(t, http.StatusNotFound, errorsx.Code(errorsx.ErrNotFound))
	assert.Equal(t, "NotFound", errorsx.Reason(errorsx.ErrNotFound))
}

// TestFromError_PreservesCause 覆盖 FromError 重建 ErrorX 时丢失原始错误链的问题.
func TestFromError_PreservesCause(t *testing.T) {
	base := &customErr{msg: "boom"}
	wrapped := fmt.Errorf("wrapped: %w", base)

	errx := errorsx.FromError(wrapped)

	// errors.Unwrap 能拿到原始错误
	assert.Equal(t, wrapped, errorsx.Unwrap(errx))
	assert.Equal(t, wrapped, errors.Unwrap(errx))

	// errors.Is / errorsx.Is 能穿透到原始错误
	assert.True(t, errors.Is(errx, base), "errors.Is should reach the wrapped cause")
	assert.True(t, errorsx.Is(errx, base), "errorsx.Is should reach the wrapped cause")
	assert.True(t, errors.Is(errx, wrapped))

	// errors.As 能穿透到原始错误的具体类型
	var ce *customErr
	assert.True(t, errors.As(errx, &ce))
	assert.Equal(t, "boom", ce.msg)

	// 既有语义仍然保留：按 Code/Reason 匹配预定义错误
	assert.True(t, errorsx.Is(errx, errorsx.ErrInternal))
	assert.Equal(t, http.StatusInternalServerError, errorsx.Code(errx))
	assert.Equal(t, string(errorsx.ErrInternal.Reason), errorsx.Reason(errx))
	assert.Contains(t, errx.Message, "wrapped: boom")
}

// TestFromError_PlainErrorChain 覆盖不带自定义类型的普通错误链.
func TestFromError_PlainErrorChain(t *testing.T) {
	base := errors.New("base error")
	wrapped := fmt.Errorf("layer1: %w", fmt.Errorf("layer2: %w", base))

	errx := errorsx.FromError(wrapped)
	assert.True(t, errors.Is(errx, base))
	assert.True(t, errorsx.Is(errx, base))
	assert.Equal(t, wrapped, errors.Unwrap(errx))
	assert.Contains(t, errx.Message, "layer2: base error")
}

// TestFromError_GRPCErrorChain 覆盖 gRPC 错误链同样被保留.
func TestFromError_GRPCErrorChain(t *testing.T) {
	grpcErr := status.New(3, "Invalid argument").Err()

	t.Run("plain-grpc-error", func(t *testing.T) {
		errx := errorsx.FromError(grpcErr)
		assert.Equal(t, http.StatusBadRequest, errx.Code)
		assert.Equal(t, "Invalid argument", errx.Message)
		assert.True(t, errors.Is(errx, grpcErr), "the original grpc error should remain in the chain")
	})

	t.Run("wrapped-grpc-error", func(t *testing.T) {
		wrapped := fmt.Errorf("wrapped: %w", grpcErr)
		errx := errorsx.FromError(wrapped)
		assert.Equal(t, http.StatusBadRequest, errx.Code)
		// grpc 的 status.FromError 在包装场景下会把整条错误链的 Error() 作为 Message
		assert.Contains(t, errx.Message, "Invalid argument")
		assert.True(t, errors.Is(errx, grpcErr), "the original grpc error should remain in the chain")
	})
}

// TestFromError_NilCause 覆盖没有 cause 时 Unwrap 返回 nil.
func TestFromError_NilCause(t *testing.T) {
	errx := errorsx.New(500, "Internal", "plain")
	assert.NoError(t, errorsx.Unwrap(errx))
	assert.False(t, errors.Is(errx, errors.New("nope")))
}

// TestErrorX_ConcurrentMutators 覆盖并发链式调用预定义错误时的数据竞争（配合 -race）.
func TestErrorX_ConcurrentMutators(t *testing.T) {
	var wg sync.WaitGroup
	for k := range 8 {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for range 200 {
				err := errorsx.ErrNotFound.WithMessage("msg-%d", k).KV("idx", "v").WithRequestID("req")
				_ = err.Error()
				_ = errorsx.ErrNotFound.Error()
				_ = errorsx.Code(errorsx.ErrNotFound)
				_ = errorsx.Reason(errorsx.ErrNotFound)
				_ = errorsx.Is(err, errorsx.ErrNotFound)
			}
		}(k)
	}
	wg.Wait()

	assert.Equal(t, "Resource not found.", errorsx.ErrNotFound.Message)
	assert.Nil(t, errorsx.ErrNotFound.Metadata)
}
