package core

import (
	"context"
	"net/http"
	"strconv"

	"github.com/LiangNing7/goutils/pkg/errorsx"
	"github.com/gin-gonic/gin"
)

// Validator 是验证函数的类型，用于对绑定的数据结构进行验证.
// 接收上下文和待验证的请求结构体指针，返回验证错误（如果有）.
type Validator[T any] func(context.Context, *T) error

// Binder 定义绑定函数的类型，用于将请求数据绑定到相应结构体.
// 接收目标结构体，返回绑定错误（如果有）.
type Binder func(any) error

// Handler 是处理函数的类型，用于处理已绑定和验证的数据.
// 接收上下文和请求结构体指针，返回处理结果和错误.
type Handler[T any, R any] func(ctx context.Context, req *T) (R, error)

// ErrorResponse 定义了错误响应的结构，
// 用于 API 请求中发生错误时返回统一的格式化错误信息.
type ErrorResponse struct {
	// 错误原因，标识错误类型
	Reason string `json:"reason,omitempty"`
	// 错误详情的描述信息
	Message string `json:"message,omitempty"`
	// 附带的元数据信息
	Metadata map[string]string `json:"metadata,omitempty"`
}

// HandleRequest 是通用的请求处理函数.
// 负责绑定请求数据、执行验证、并调用实际的业务处理逻辑函数.
func HandleRequest[T any, R any](c *gin.Context, binder Binder, handler Handler[T, R], validators ...Validator[T]) {
	var request T

	// 绑定和验证请求数据
	if err := ReadRequest(c, &request, binder, validators...); err != nil {
		WriteResponse(c, nil, err)
		return
	}

	// 调用实际的业务逻辑处理函数
	response, err := handler(c.Request.Context(), &request)
	WriteResponse(c, response, err)
}

// HandleEmptyRequest 是处理空请求的快捷函数.
func HandleEmptyRequest[T any, R any](c *gin.Context, handler Handler[T, R], validators ...Validator[T]) {
	emptyBinder := func(_ any) error { return nil }
	HandleRequest(c, emptyBinder, handler, validators...)
}

// HandleJSONRequest 是处理 JSON 请求的快捷函数.
func HandleJSONRequest[T any, R any](c *gin.Context, handler Handler[T, R], validators ...Validator[T]) {
	HandleRequest(c, c.ShouldBindJSON, handler, validators...)
}

// HandleQueryRequest 是处理 Query 参数请求的快捷函数.
func HandleQueryRequest[T any, R any](c *gin.Context, handler Handler[T, R], validators ...Validator[T]) {
	HandleRequest(c, c.ShouldBindQuery, handler, validators...)
}

// HandleUriRequest 是处理 URI 请求的快捷函数.
func HandleUriRequest[T any, R any](c *gin.Context, handler Handler[T, R], validators ...Validator[T]) {
	HandleRequest(c, c.ShouldBindUri, handler, validators...)
}

// ReadRequest 是用于绑定和验证请求数据的通用工具函数.
// - 它负责调用绑定函数绑定请求数据.
// - 如果目标类型实现了 Default 接口，会调用其 Default 方法设置默认值.
// - 最后执行传入的验证器对数据进行校验.
//
// 注意 Default() 的调用时机：它在 binder 之后调用，且签名不接收 context.
// 这意味着 Default() 中设置的值会**覆盖**请求中显式传入的同名字段值，
// 并且无法根据每次请求的上下文（如登录用户、请求参数）计算默认值。
// 因此 Default() 只应设置「请求未提供时」才有意义的固定默认值；
// 若需要「请求优先」，请在验证器中处理，而不是依赖 Default()。
func ReadRequest[T any](c *gin.Context, rq *T, binder Binder, validators ...Validator[T]) error {
	// 调用绑定函数绑定请求数据
	if err := binder(rq); err != nil {
		return errorsx.ErrBind.WithMessage("%s", err.Error())
	}

	// 如果数据结构实现了 Default 接口，则调用它的 Default 方法
	if defaulter, ok := any(rq).(interface{ Default() }); ok {
		defaulter.Default()
	}

	// 执行所有验证函数
	for _, validate := range validators {
		if validate == nil { // 跳过 nil 的验证器
			continue
		}
		if err := validate(c.Request.Context(), rq); err != nil {
			return err
		}
	}

	return nil
}

// ShouldBindJSON 使用 JSON 格式的绑定函数绑定请求参数并执行验证。
func ShouldBindJSON[T any](c *gin.Context, rq *T, validators ...Validator[T]) error {
	return ReadRequest(c, rq, c.ShouldBindJSON, validators...)
}

// ShouldBindQuery 使用 Query 格式的绑定函数绑定请求参数并执行验证。
func ShouldBindQuery[T any](c *gin.Context, rq *T, validators ...Validator[T]) error {
	return ReadRequest(c, rq, c.ShouldBindQuery, validators...)
}

// ShouldBindUri 使用 URI 格式的绑定函数绑定请求参数并执行验证。
func ShouldBindUri[T any](c *gin.Context, rq *T, validators ...Validator[T]) error {
	return ReadRequest(c, rq, c.ShouldBindUri, validators...)
}

// WriteResponse 是通用的响应函数.
// 它会根据是否发生错误，生成成功响应或标准化的错误响应.
//
// 关于错误状态码：只有 2xx..5xx（200-599）的状态码才会被原样写回。
// 其余状态码（0、1xx、>599）都会回退为 500，并把原始 Code 写入
// 响应体的 metadata["original_code"] 中，便于排查。
// 这样既避免了 Code==0 时静默返回 HTTP 200 的错误响应体，
// 也避免了 Code 为 99/1000 时 gin/net/http 的 "invalid WriteHeader code" panic，
// 以及 1xx 这类不能作为最终响应的状态码被写出。
func WriteResponse(c *gin.Context, data any, err error) {
	if err != nil {
		// 如果发生错误，生成错误响应
		errx := errorsx.FromError(err) // 提取错误详细信息

		code := errx.Code
		resp := ErrorResponse{
			Reason:   errx.Reason,
			Message:  errx.Message,
			Metadata: errx.Metadata,
		}

		if !validResponseStatus(code) {
			resp.Metadata = withOriginalCode(errx.Metadata, code)
			code = http.StatusInternalServerError
		}

		c.JSON(code, resp)
		return
	}

	// 如果没有错误，返回成功响应
	c.JSON(http.StatusOK, data)
}

// validResponseStatus 判断 code 能否作为最终的 HTTP 响应状态码.
// 只有成功(2xx)到服务端错误(5xx)之间的状态码才能作为最终响应；
// 0、1xx（信息响应，必须跟随一个最终响应）以及大于 599 的非标准状态码都不合法.
func validResponseStatus(code int) bool {
	return code >= http.StatusOK && code <= 599
}

// withOriginalCode 返回一份 metadata 副本，并写入原始状态码。
// 这里返回副本而不是原地修改，避免污染错误对象自带的（甚至是被多个请求共享的）metadata.
func withOriginalCode(md map[string]string, original int) map[string]string {
	out := make(map[string]string, len(md)+1)
	for k, v := range md {
		out[k] = v
	}
	out["original_code"] = strconv.Itoa(original)
	return out
}
