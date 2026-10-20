package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/LiangNing7/goutils/pkg/errorsx"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

// --- WriteResponse：非法状态码 ---

func TestWriteResponse_ValidCodesArePassedThrough(t *testing.T) {
	for _, code := range []int{200, 201, 400, 404, 409, 500, 599} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			c, w := newTestContext(t)

			require.NotPanics(t, func() {
				WriteResponse(c, nil, errorsx.New(code, "SomeReason", "some message"))
			})

			assert.Equal(t, code, w.Code)

			var body ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "SomeReason", body.Reason)
			assert.Equal(t, "some message", body.Message)
		})
	}
}

func TestWriteResponse_InvalidCodesFallBackTo500(t *testing.T) {
	// 0 之前会静默回落 HTTP 200；99/1000 会让 httptest/net/http panic；100 是 1xx；
	// 600 是非标准状态码. 这些都应当回退 500.
	for _, code := range []int{0, 99, 100, 1000, -1, 600} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			c, w := newTestContext(t)

			require.NotPanics(t, func() {
				WriteResponse(c, nil, errorsx.New(code, "SomeReason", "some message"))
			})

			assert.Equal(t, http.StatusInternalServerError, w.Code)

			var body ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "SomeReason", body.Reason)
			assert.Equal(t, "some message", body.Message)
			// 原始 Code 保留在 metadata 中，便于排查
			assert.Equal(t, strconv.Itoa(code), body.Metadata["original_code"])
		})
	}
}

func TestWriteResponse_KeepsExistingMetadata(t *testing.T) {
	c, w := newTestContext(t)

	WriteResponse(c, nil, errorsx.New(99, "R", "m").WithMetadata(map[string]string{"request_id": "abc"}))

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "abc", body.Metadata["request_id"])
	assert.Equal(t, "99", body.Metadata["original_code"])
}

func TestWriteResponse_DoesNotMutateErrorMetadata(t *testing.T) {
	c, _ := newTestContext(t)

	errx := errorsx.New(0, "R", "m").WithMetadata(map[string]string{"k": "v"})
	WriteResponse(c, nil, errx)

	// 不能被注入 original_code
	assert.Equal(t, map[string]string{"k": "v"}, errx.Metadata)
}

func TestWriteResponse_Success(t *testing.T) {
	c, w := newTestContext(t)

	WriteResponse(c, gin.H{"status": "ok"}, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
}

// --- ReadRequest：Default() 的时机与错误处理 ---

type defaultedReq struct {
	Name  string
	Retry int
}

// Default 无条件覆盖 Name，用于固化「Default() 在 bind 之后调用」的当前语义.
func (r *defaultedReq) Default() {
	r.Name = "default-name"
	if r.Retry == 0 {
		r.Retry = 3
	}
}

func TestReadRequest_DefaultIsCalledAfterBind(t *testing.T) {
	c, _ := newTestContext(t)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"from-request"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var rq defaultedReq
	require.NoError(t, ReadRequest(c, &rq, c.ShouldBindJSON))

	// 当前语义（已固化）：Default() 在 bind 之后调用且不接收 ctx，
	// 因此 Default 中设置的值会覆盖请求里显式传入的值.
	assert.Equal(t, "default-name", rq.Name)
	// 请求未提供的字段由 Default 补上
	assert.Equal(t, 3, rq.Retry)
}

func TestReadRequest_ValidatorSeesDefaultedValues(t *testing.T) {
	c, _ := newTestContext(t)

	var rq defaultedReq
	sawRetry := -1
	validator := func(_ context.Context, r *defaultedReq) error {
		sawRetry = r.Retry
		if r.Retry != 3 {
			return errors.New("retry has not been defaulted")
		}
		return nil
	}

	require.NoError(t, ReadRequest(c, &rq, func(any) error { return nil }, validator))
	assert.Equal(t, 3, sawRetry)
}

func TestReadRequest_ValidatorErrorIsReturned(t *testing.T) {
	c, _ := newTestContext(t)

	wantErr := errors.New("invalid argument")
	err := ReadRequest(c, &defaultedReq{}, func(any) error { return nil },
		func(context.Context, *defaultedReq) error { return wantErr })

	assert.ErrorIs(t, err, wantErr)
}

func TestReadRequest_SkipsNilValidator(t *testing.T) {
	c, _ := newTestContext(t)

	var nilValidator Validator[defaultedReq]

	assert.NotPanics(t, func() {
		require.NoError(t, ReadRequest(c, &defaultedReq{}, func(any) error { return nil }, nilValidator))
	})
}

func TestReadRequest_BinderErrorKeepsRawMessage(t *testing.T) {
	c, _ := newTestContext(t)

	// 错误文本里带 % 时不能被当成格式化占位符（README 里曾写成不安全的调用方式）
	bindErr := errors.New("bind failed: 100% bad")
	err := ReadRequest(c, &defaultedReq{}, func(any) error { return bindErr })

	require.Error(t, err)
	assert.ErrorIs(t, err, errorsx.ErrBind)
	assert.Contains(t, err.Error(), "bind failed: 100% bad")
}
