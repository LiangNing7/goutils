# core

> core 包提供了很多请求解析、请求处理函数。这些函数可以满足，不同的请求解析、请求处理场景。
>
> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/core
> ```

## core

以 `HandleJSONRequest` 为例：

```go
// HandleJSONRequest 是处理 JSON 请求的快捷函数.
func HandleJSONRequest[T any, R any](c *gin.Context, handler Handler[T, R], validators ...Validator[T]) {
    HandleRequest(c, c.ShouldBindJSON, handler, validators...)
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
```

上述代码中，`HandleJSONRequest` 函数是基于 `HandleRequest` 封装的一个语法糖函数，用来简化调用方调用时的参数输入，提高调用效率，减小调用复杂度。语法糖函数 `HandleJSONRequest`、`HandleQueryRequest`、`HandleUriRequest` 调用了 `HandleRequest` 函数，在调用 `HandleRequest` 时，传入了对应的 `gin.Context` 类型解析请求参数的方法。

在 `HandleRequest` 函数中，会先调用 `ReadRequest` 从请求中解析出请求参数，所有的请求信息都保存在 `*gin.Context` 类型的变量 c 中。解析完请求之后会调用传入的 Handler 方法，进行请求处理。

### 请求解析 `ReadRequest` 函数

在解析 HTTP 请求参数时，所有的 API 接口均需要对请求参数进行默认值设置、参数校验。`ReadRequest` 实现代码如下所示：

```go
// ReadRequest 是用于绑定和验证请求数据的通用工具函数.
// - 它负责调用绑定函数绑定请求数据.
// - 如果目标类型实现了 Default 接口，会调用其 Default 方法设置默认值.
// - 最后执行传入的验证器对数据进行校验.
func ReadRequest[T any](c *gin.Context, rq *T, binder Binder, validators ...Validator[T]) error {
    // 调用绑定函数绑定请求数据
    if err := binder(rq); err != nil {
        return errorsx.ErrBind.WithMessage(err.Error())
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
```

`ReadRequest` 是一个通用的、泛型化的工具函数，用于对请求数据进行参数绑定、初始化默认值以及进行请求参数校验，其功能设计清晰且非常灵活，适应多种场景。`ReadRequest` 函数会根据传入的绑定方法 `Binder`，将请求参数绑定到传入的结构体类型变量 `rq` 中。

`ReadRequest` 函数也会判断传入的 `rq` 结构体变量是否实现了 `Default()` 方法，如果实现了，则调用 `rq` 的 `Default()` 方法，用来对请求参数 `rq` 进行参数默认值设置操作。

`ReadRequest` 函数还接收一个可变长参数 `validators`，`validators` 列表中包含了 `rq` 结构体变量的校验方法列表。`ReadRequest` 函数会遍历并执行所有的校验方法，当其中一个校验方法返回校验失败错误时，`ReadRequest` 函数结束运行，并返回错误。

**Usage**：只展示使用例子，具体结构体未展现。

```go
import "github.com/LiangNing7/goutils/pkg/core"

// CreatePost 创建博客帖子.
func (h *Handler) CreatePost(c *gin.Context) {
	core.HandleJSONRequest(c, h.biz.PostV1().Create)
}

// UpdatePost 更新博客帖子.
func (h *Handler) UpdatePost(c *gin.Context) {
	core.HandleJSONRequest(c, h.biz.PostV1().Update)
}

// DeletePost 删除博客帖子.
func (h *Handler) DeletePost(c *gin.Context) {
	core.HandleJSONRequest(c, h.biz.PostV1().Delete)
}

// GetPost 获取博客帖子.
func (h *Handler) GetPost(c *gin.Context) {
	core.HandleUriRequest(c, h.biz.PostV1().Get)
}

// ListPosts 列出用户的所有博客帖子.
func (h *Handler) ListPost(c *gin.Context) {
	core.HandleQueryRequest(c, h.biz.PostV1().List)
}
```

### 请求返回 `WriteResponse` 函数

```go
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

// WriteResponse 是通用的响应函数.
// 它会根据是否发生错误，生成成功响应或标准化的错误响应.
func WriteResponse(c *gin.Context, data any, err error) {
    if err != nil {
        // 如果发生错误，生成错误响应
        errx := errorsx.FromError(err) // 提取错误详细信息
        c.JSON(errx.Code, ErrorResponse{
            Reason:   errx.Reason,
            Message:  errx.Message,
            Metadata: errx.Metadata,
        })
        return
    }

    // 如果没有错误，返回成功响应
    c.JSON(http.StatusOK, data)
}
```

`WriteResponse` 方法会判断 err 是否是 nil，如果不是 nil，则将其解析为 `errorsx.ErrorX` 类型的变量 errx，并读取 errx 变量中的 Code、Reason、Message、Metadata 字段。Code 字段的值用来设置 HTTP 状态码，其他字段的值用来构建 `ErrorResponse` 类型对象，并编码为 JSON 格式，保存在返回体中。在接口报错时，返回结构固定的错误，可以降低客户端处理错误代码实现复杂度。

**Usage**：只展示使用例子，具体结构体未展现。

```go
import "github.com/LiangNing7/goutils/pkg/core"

core.WriteResponse(c, apiv1.HealthzResponse{
    Status:    apiv1.ServiceStatus_Healthy,
    Timestamp: time.Now().Format(time.DateTime),
}, nil)
```

## config

`OnInitialize`，用于设置配置文件和环境变量的读取方式。

```go
// OnInitialize 返回一个初始化函数，用于设置配置文件和环境变量的读取方式。
// - configFile: 指向配置文件路径的指针，可通过命令行参数指定。
// - envPrefix: 环境变量前缀，用于过滤并命名该应用的环境变量。
// - loadDirs: 配置文件搜索目录列表，当未指定configFile时使用。
// - defaultConfigName: 默认配置文件名（不含扩展名）。
func OnInitialize(configFile *string, envPrefix string, loadDirs []string, defaultConfigName string) func() {
	return func() {
		// 如果通过命令行指定了配置文件路径，则优先使用该路径.
		if configFile != nil {
			// 从命令行选项指定的配置文件中读取
			viper.SetConfigFile(*configFile)
		} else {
			// 否则，将各个目录加入搜索路径，依次查找配置文件.
			for _, dir := range loadDirs {
				// 将 dir 目录加入到配置文件的搜索路径.
				viper.AddConfigPath(dir)
			}

			// 设置配置文件格式为 YAML.
			viper.SetConfigType("yaml")

			// 配置文件名称（没有文件扩展名）.
			viper.SetConfigName(defaultConfigName)
		}

		// 读取匹配的环境变量.
		viper.AutomaticEnv()

		// 设置环境变量前缀.
		// 例如：envPrefix="MINIBLOG"，则只读取以 MINIBLOG_ 开头的变量.
		viper.SetEnvPrefix(envPrefix)

		// 将 key 字符串中 '.' 和 '-' 替换为 '_'
		replacer := strings.NewReplacer(".", "_", "-", "_")
		viper.SetEnvKeyReplacer(replacer)

		// 读取配置文件。如果指定了配置文件名，则使用指定的配置文件，否则在注册的搜索路径中搜索
		_ = viper.ReadInConfig()
	}
}
```

**Usage**：一般用于启动应用程序。

```go
import "github.com/LiangNing7/goutils/pkg/core"

// NewCommand 创建一个 *cobra.Command 对象，用于启动应用程序.
func NewCommand() *cobra.Command {
    cobra.OnInitialize(core.OnInitialize(
        configFile,
        envPrefix,
        loadDirs,
        defaultConfigName,
    ))
}
```

## copier

### TypeConverters

```go
// TypeConverters 定义时间类型转换器，用于 copier 的深度拷贝.
// 主要用于在 Go 原生的 time.Time 与 Protobuf 内置的 Timestamp 之间进行相互转换.
func TypeConverters() []copier.TypeConverter {
	return []copier.TypeConverter{
		{
			// 源类型为 time.Time，目标类型为 *timestamppb.Timestamp.
			SrcType: time.Time{},
			DstType: &timestamppb.Timestamp{},
			// 当执行拷贝时，调用此函数将 time.Time 转换为 *timestamppb.Timestamp.
			Fn: func(src any) (any, error) {
				s, ok := src.(time.Time)
				if !ok {
					return nil, errors.New("source type not matching")
				}
				return timestamppb.New(s), nil
			},
		},
		{
			// 源类型为 *timestamppb.Timestamp，目标类型为 time.Time.
			SrcType: &timestamppb.Timestamp{},
			DstType: time.Time{},
			// 当执行拷贝时，调用此函数将 *timestamppb.Timestamp 转换为 time.Time.
			Fn: func(src any) (any, error) {
				s, ok := src.(*timestamppb.Timestamp)
				if !ok {
					return nil, errors.New("source type not matching")
				}
				return s.AsTime(), nil
			},
		},
	}
}
```

### CopyWithConverters

```go
// CopyWithConverters 使用自定义转换器，支持深拷贝并忽略零值字段.
// to: 目标结构体指针; from: 源结构体指针.
func CopyWithConverters(to any, from any) error {
	return copier.CopyWithOption(
		to,
		from,
		copier.Option{
			IgnoreEmpty: true,             // 忽略源中空值字段.
			DeepCopy:    true,             // 启用深度拷贝.
			Converters:  TypeConverters(), // 应用自定义类型转换器
		})
}
```

### Copy

```go
// Copy 普通拷贝，不应用自定义转换，只做浅拷贝并赋值同名字段.
func Copy(to any, from any) error {
	return copier.Copy(to, from)
}
```

### 总结

* **`TypeConverters`**：配置了两组 `time.Time ↔ *timestamppb.Timestamp` 的互转函数；
* **`CopyWithConverters`**：在常规拷贝基础上，开启深拷贝、忽略空值，并使用自定义转换器，适合 Protobuf 与 Go 模型混合的场景；
* **`Copy`**：最简单的同名字段浅拷贝，不做类型转换，适合字段类型一致时使用。
