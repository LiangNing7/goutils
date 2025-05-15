# errorsx


> 此 errorsx 包提供了一种将自定义错误映射到 HTTP 和 gRPC 状态代码的便捷方法。这有助于确保不同服务架构之间错误处理的一致性。
>
> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/errorsx
> ```

在开发自定义包时，可以从包命名上避免与标准库包名冲突。建议将可能冲突的包命名为 `<冲突包原始名>x`**，**其名称中的 “x” 代表扩展（extended）或实验（experimental）。这种命名方式是一种扩展命名约定，通常用于表示此包是对标准库中已有包功能的扩展或补充。需要注意的是，这并非 Go 语言的官方规范，而是开发者为了防止命名冲突、增强语义所采用的命名方式。

项目的错误包命名为 errorsx，为保持命名一致性，定义了一个名为 ErrorX 的结构体，用于描述错误信息，具体定义如下：`pkg/errorsx/errorsx.go`

```go
// ErrorX 定义了 OneX 项目体系中使用的错误类型，用于描述错误的详细信息.
type ErrorX struct {
    // Code 表示错误的 HTTP 状态码，用于与客户端进行交互时标识错误的类型.
    Code int `json:"code,omitempty"`

    // Reason 表示错误发生的原因，通常为业务错误码，用于精准定位问题.
    Reason string `json:"reason,omitempty"`

    // Message 表示简短的错误信息，通常可直接暴露给用户查看.
    Message string `json:"message,omitempty"`

    // Metadata 用于存储与该错误相关的额外元信息，可以包含上下文或调试信息.
    Metadata map[string]string `json:"metadata,omitempty"`
}
```

ErrorX 是一个错误类型，因此需要实现 Error 方法：

```go
// Error 实现 error 接口中的 `Error` 方法.
func (err *ErrorX) Error() string {
    return fmt.Sprintf("error: code = %d reason = %s message = %s metadata = %v", err.Code, err.Reason, err.Message, err.Metadata)
}
```

Error() 返回的错误信息中，包含了 HTTP 状态码、错误发生的原因、错误信息和额外的错误元信息。通过这些详尽的错误信息返回，帮助开发者快速定位错误。

在 Go 项目开发中，发生错误的原因有很多，大多数情况下，开发者希望将真实的错误信息返回给用户。因此，还需要提供一个方法用来设置 ErrorX 结构体中的 Message 字段。同样的，还需要提供设置 Metadata 字段的方法。为了满足上述诉求，给 ErrorX 增加 WithMessage、WithMetadata、KV 三个方法。实现方式如下所示：

```go
// WithMessage 设置错误的 Message 字段.
func (err *ErrorX) WithMessage(format string, args ...any) *ErrorX {
    err.Message = fmt.Sprintf(format, args...)
    return err
}

// WithMetadata 设置元数据.
func (err *ErrorX) WithMetadata(md map[string]string) *ErrorX {
    err.Metadata = md
    return err
}

// KV 使用 key-value 对设置元数据.
func (err *ErrorX) KV(kvs ...string) *ErrorX {
    if err.Metadata == nil {
        err.Metadata = make(map[string]string) // 初始化元数据映射
    }

    for i := 0; i < len(kvs); i += 2 {
        // kvs 必须是成对的
        if i+1 < len(kvs) {
            err.Metadata[kvs[i]] = kvs[i+1]
        }
    }
    return err
}
```

上述代码中，设置 Message、Metadata 字段的方法名分别为 WithMessage、WithMetadata。WithXXX，在 Go 项目开发中是一种很常见的命名方式，寓意是：设置 XXX。KV 方法则以追加的方式给 Metadata 增加键值对。

WithMessage、WithMetadata、KV 都返回了 *ErrorX 类型的实例，目的是为了实现链式调用，例如：

```go
err := new(ErrorX)
err.WithMessage("Message").WithMetadata(map[string]string{"key":"value"})
```

在 Go 项目开发中，链式调用（chained method calls）是一种常见的设计模式，该模式通过在方法中返回对象自身，使多个方法调用可以连续进行。链式调用的好处在于：简化代码、提高可读性、减少错误可能性和增强扩展性，尤其是在对象构造或逐步修改操作时，非常高效直观。合理使用链式调用可以显著提升代码的质量和开发效率，同时让接口设计更加优雅。

errorsx 包的设计目标不仅适用于 HTTP 接口的错误返回，还适用于 gRPC 接口的错误返回。因此，ErrorX 结构体还实现了 `GRPCStatus()` 方法。`GRPCStatus()` 方法的作用是将自定义错误类型 ErrorX 转换为 gRPC 的 status.Status 类型，用于生成 gRPC 标准化的错误返回信息（包括错误码、错误消息及详细错误信息），从而满足 gRPC 框架的错误处理要求。`GRPCStatus()` 方法实现如下：

```go
// GRPCStatus 返回 gRPC 状态表示.
func (err *ErrorX) GRPCStatus() *status.Status {
    details := errdetails.ErrorInfo{Reason: err.Reason, Metadata: err.Metadata}
    s, _ := status.New(httpstatus.ToGRPCCode(err.Code), err.Message).WithDetails(&details)
    return s
}
```

在 Go 项目开发中，通常需要将一个 error 类型的错误 err，解析为 `*ErrorX` 类型，并获取 `*ErrorX` 中的 Code 字段和 Reason 字段的值。Code 字段可用来设置 HTTP 状态码，Reason 字段可用来判断错误类型。为此，errorsx 包实现了 FromError、Code、Reason 方法，具体实现代码如下：

```go
// Code 返回错误的 HTTP 代码.
func Code(err error) int {
    if err == nil {
        return http.StatusOK //nolint:mnd
    }
    return FromError(err).Code
}

// Reason 返回特定错误的原因.
func Reason(err error) string {
    if err == nil {
        return ErrInternal.Reason
    }
    return FromError(err).Reason
}

// FromError 尝试将一个通用的 error 转换为自定义的 *ErrorX 类型.
func FromError(err error) *ErrorX {
    // 如果传入的错误是 nil，则直接返回 nil，表示没有错误需要处理.
    if err == nil {
        return nil
    }

    // 检查传入的 error 是否已经是 ErrorX 类型的实例.
    // 如果错误可以通过 errors.As 转换为 *ErrorX 类型，则直接返回该实例.
    if errx := new(ErrorX); errors.As(err, &errx) {
        return errx
    }

    // gRPC 的 status.FromError 方法尝试将 error 转换为 gRPC 错误的 status 对象.
    // 如果 err 不能转换为 gRPC 错误（即不是 gRPC 的 status 错误），
    // 则返回一个带有默认值的 ErrorX，表示是一个未知类型的错误.
    gs, ok := status.FromError(err)
    if !ok {
        return New(ErrInternal.Code, ErrInternal.Reason, "%s",err.Error())
    }

    // 如果 err 是 gRPC 的错误类型，会成功返回一个 gRPC status 对象（gs）.
    // 使用 gRPC 状态中的错误代码和消息创建一个 ErrorX.
    ret := New(httpstatus.FromGRPCCode(gs.Code()), ErrInternal.Reason, "%s",gs.Message())

    // 遍历 gRPC 错误详情中的所有附加信息（Details）.
    for _, detail := range gs.Details() {
        if typed, ok := detail.(*errdetails.ErrorInfo); ok {
            ret.Reason = typed.Reason
            return ret.WithMetadata(typed.Metadata)
        }
    }

    return ret
}
```

在 Go 项目开发中，经常还要对比一个 error 类型的错误 err 是否是某个预定义错误，因此 `*ErrorX` 也需要实现一个 `Is` 方法，`Is` 方法实现如下：

```go
// Is 判断当前错误是否与目标错误匹配.
// 它会递归遍历错误链，并比较 ErrorX 实例的 Code 和 Reason 字段.
// 如果 Code 和 Reason 均相等，则返回 true；否则返回 false.
func (err *ErrorX) Is(target error) bool {
    if errx := new(ErrorX); errors.As(target, &errx) {
        return errx.Code == err.Code && errx.Reason == err.Reason
    }
    return false
}
```

Is 方法中，通过对比 Code 和 Reason 字段，来判断 target 错误是否是指定的预定义错误。注意，Is 方法中，没有对比 Message 字段的值，这是因为 Message 字段的值通常是动态的，而错误类型的定义不依赖于 Message。

至此，成功开发了一个满足项目需求的错误包 errorsx，代码完整实现见 goutils 项目的 [`pkg/errorsx/errorsx.go`](https://github.com/LiangNing7/goutils/blob/main/pkg/errorsx/errorsx.go) 文件。

还可以在 [`pkg/errorsx/code.go`](https://github.com/LiangNing7/goutils/blob/main/pkg/errorsx/code.go) 文件中预定义一些错误：

```go
package errorsx

import "net/http"

// errorsx 预定义标准的错误.
var (
	// OK 代表请求成功.
	OK = &ErrorX{Code: http.StatusOK, Message: ""}

	// ErrInternal 表示所有未知的服务器端错误.
	ErrInternal = &ErrorX{Code: http.StatusInternalServerError, Reason: "InternalError", Message: "Internal server error."}

	// ErrNotFound 表示资源未找到.
	ErrNotFound = &ErrorX{Code: http.StatusNotFound, Reason: "NotFound", Message: "Resource not found."}

	// ErrBind 表示请求体绑定错误.
	ErrBind = &ErrorX{Code: http.StatusBadRequest, Reason: "BindError", Message: "Error occurred while binding the request body to the struct."}

	// ErrInvalidArgument 表示参数验证失败.
	ErrInvalidArgument = &ErrorX{Code: http.StatusBadRequest, Reason: "InvalidArgument", Message: "Argument verification failed."}

	// ErrUnauthenticated 表示认证失败.
	ErrUnauthenticated = &ErrorX{Code: http.StatusUnauthorized, Reason: "Unauthenticated", Message: "Unauthenticated."}

	// ErrPermissionDenied 表示请求没有权限.
	ErrPermissionDenied = &ErrorX{Code: http.StatusForbidden, Reason: "PermissionDenied", Message: "Permission denied. Access to the requested resource is forbidden."}

	// ErrOperationFailed 表示操作失败.
	ErrOperationFailed = &ErrorX{Code: http.StatusConflict, Reason: "OperationFailed", Message: "The requested operation has failed. Please try again later."}
)
```

最后，为了更优雅的包裹错误链，还需实现：三大函数（`Is`、`As`、`Unwrap`），位于 [`pkg/errorsx/wrap.go`](https://github.com/LiangNing7/goutils/blob/main/pkg/errorsx/wrap.go) 文件中

```go
package errorsx

import (
	"errors"
)

// Is reports whether any error in err's chain matches target.
//
// The chain consists of err itself followed by the sequence of errors obtained by
// repeatedly calling Unwrap.
//
// An error is considered to match a target if it is equal to that target or if
// it implements a method Is(error) bool such that Is(target) returns true.
func Is(err, target error) bool { return errors.Is(err, target) }

// As finds the first error in err's chain that matches target, and if so, sets
// target to that error value and returns true.
//
// The chain consists of err itself followed by the sequence of errors obtained by
// repeatedly calling Unwrap.
//
// An error matches target if the error's concrete value is assignable to the value
// pointed to by target, or if the error has a method As(interface{}) bool such that
// As(target) returns true. In the latter case, the As method is responsible for
// setting target.
//
// As will panic if target is not a non-nil pointer to either a type that implements
// error, or to any interface type. As returns false if err is nil.
func As(err error, target interface{}) bool { return errors.As(err, target) }

// Unwrap returns the result of calling the Unwrap method on err, if err's
// type contains an Unwrap method returning error.
// Otherwise, Unwrap returns nil.
func Unwrap(err error) error {
	return errors.Unwrap(err)
}
```




