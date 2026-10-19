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
```

`ReadRequest` 是一个通用的、泛型化的工具函数，用于对请求数据进行参数绑定、初始化默认值以及进行请求参数校验，其功能设计清晰且非常灵活，适应多种场景。`ReadRequest` 函数会根据传入的绑定方法 `Binder`，将请求参数绑定到传入的结构体类型变量 `rq` 中。

`ReadRequest` 函数也会判断传入的 `rq` 结构体变量是否实现了 `Default()` 方法，如果实现了，则调用 `rq` 的 `Default()` 方法，用来对请求参数 `rq` 进行参数默认值设置操作。

> **关于 `Default()` 的语义（重要）**：`Default()` 在绑定（bind）**之后**调用，且不接收 `context`。因此：
> 1. `Default()` 里设置的值会覆盖请求中显式传入的值（即 `Default` 优先，而不是请求优先）；
> 2. 无法根据每次请求的上下文计算默认值。
>
> 该行为已被测试固化（`TestReadRequest_DefaultIsCalledAfterBind`）。修改调用顺序（例如改为 bind 之前调用）会反转"请求值 vs 默认值"的优先级，属于破坏性变更，因此这里保持不变。

绑定失败时返回的是 `errorsx.ErrBind.WithMessage("%s", err.Error())`：错误文本被当作**格式化参数**而不是格式字符串，因此即使错误信息里包含 `%`（例如 `100% bad`）也不会被误当成占位符。

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
```

`WriteResponse` 方法会判断 err 是否是 nil，如果不是 nil，则将其解析为 `errorsx.ErrorX` 类型的变量 errx，并读取 errx 变量中的 Code、Reason、Message、Metadata 字段。Code 字段的值用来设置 HTTP 状态码，其他字段的值用来构建 `ErrorResponse` 类型对象，并编码为 JSON 格式，保存在返回体中。在接口报错时，返回结构固定的错误，可以降低客户端处理错误代码实现复杂度。

**状态码校验**：`errx.Code` 只有在 `200 <= Code <= 599` 时才被原样透出。其余情况一律回退为 `500`，并把原始 Code 写入 `metadata["original_code"]`（原有的 metadata 会被保留，且不会修改错误对象自带的 metadata）。触发回退的典型值：

| Code | 未校验时的行为 | 校验后 |
| --- | --- | --- |
| `0` | 静默返回 HTTP 200 + 错误响应体（客户端以为成功） | HTTP 500 |
| `99` / `1000` | `panic: invalid WriteHeader code xxx` | HTTP 500 |
| `100`（1xx） | HTTP 100（信息响应不能作为最终响应） | HTTP 500 |
| `600` 及更大 | 非标准状态码 | HTTP 500 |

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

> **Deprecated**：该函数是配置加载逻辑的早期实现，缺少配置文件格式选择、配置热加载与读取失败的上报能力；`pkg/app` 中已有更完善的实现（`app.AddConfigFlag` 等）。新代码请使用 `pkg/app` 提供的初始化方式，本函数仅为兼容历史调用方保留。

```go
// OnInitialize 返回一个初始化函数，用于设置配置文件和环境变量的读取方式。
// - configFile: 指向配置文件路径的指针，可通过命令行参数指定。
// - envPrefix: 环境变量前缀，用于过滤并命名该应用的环境变量。
// - loadDirs: 配置文件搜索目录列表，当未指定configFile时使用。
// - defaultConfigName: 默认配置文件名（不含扩展名）。
func OnInitialize(configFile *string, envPrefix string, loadDirs []string, defaultConfigName string) func() {
	return func() {
		// 只有当命令行显式指定了「非空」的配置文件路径时才使用该路径.
		// 注意：configFile 通常由 pflag 注入，永远是非 nil 指针，但可能指向空字符串，
		// 因此必须判断 *configFile 是否为空，而不能只判断指针是否为 nil.
		if configFile != nil && *configFile != "" {
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

		// 读取配置文件。如果指定了配置文件名，则使用指定的配置文件，否则在注册的搜索路径中搜索.
		if err := viper.ReadInConfig(); err != nil {
			// 「找不到配置文件」是可以接受的：配置可以全部来自环境变量.
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) {
				// 其余错误（如配置文件解析失败）不能静默吞掉；
				// 由于 OnInitialize 返回的 func() 无法返回 error，这里显式输出到 stderr.
				fmt.Fprintf(os.Stderr, "core.OnInitialize: failed to read config file: %v\n", err)
			}
		}
	}
}
```

注意两点行为变化（均为修复缺陷）：

1. `configFile` 由 pflag 注入时永远是非 nil 指针，但可能指向空字符串。旧实现只判断指针是否为 nil，导致空路径被传给 `viper.SetConfigFile("")`，从而**根本不会去 `loadDirs` 里搜索配置文件**。现在改为判断 `*configFile != ""`。
2. `viper.ReadInConfig()` 的返回值不再被无条件丢弃：「配置文件不存在」按可接受情况处理，其余错误（如配置文件解析失败）会输出到 stderr。

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

`copier` 相关函数用于在 Go 结构体之间做深拷贝，并在需要时应用类型转换器。它们都要求 `to` 是**目标结构体（或变量）的指针**。

### TypeConverters

`TypeConverters` 返回内置的转换器：Go 的 `time.Time` ↔ `*timestamppb.Timestamp`，以及 GORM 的 `datatypes.JSON` ↔ `*structpb.Struct`。

```go
// TypeConverters 定义自定义转换器，用于 copier 的深度拷贝.
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
		// 另外两组：datatypes.JSON -> *structpb.Struct 与 *structpb.Struct -> datatypes.JSON，
		// 分别通过 json.Unmarshal + structpb.NewStruct、json.Marshal(s.AsMap()) 实现。
	}
}
```

### CopyWithConverters / CopyWithConvertersStrict

```go
// CopyWithConverters 使用默认转换器，支持深拷贝并忽略零值字段.
// to: 目标结构体指针; from: 源结构体指针.
//
// 注意：该函数有意忽略源中的零值（false / 0 / "" / nil），因此在「部分更新」场景下
// 无法把目标字段显式置为 false、0 或空字符串。如果需要拷贝零值，请使用
// CopyWithConvertersStrict 或 CopyWithOptions(..., WithIgnoreEmpty(false)).
func CopyWithConverters(to any, from any) error {
	return CopyWithOptions(to, from)
}

// CopyWithConvertersStrict 使用默认转换器做深拷贝，且不忽略零值字段.
// 与 CopyWithConverters 的唯一区别是：源中的零值（false / 0 / ""）也会被拷贝到目标.
// 适合「整对象替换 / 重置」以及部分更新中需要把字段显式置零的场景.
// to: 目标结构体指针; from: 源结构体指针.
func CopyWithConvertersStrict(to any, from any) error {
	return CopyWithOptions(to, from, WithIgnoreEmpty(false))
}
```

两个函数的区别与适用场景：

| 函数 | 源中的零值（`false` / `0` / `""` / `nil`） | 适用场景 |
| --- | --- | --- |
| `CopyWithConverters` | **忽略**，不覆盖目标字段 | 部分更新（PATCH）：只想更新请求里真正提供的字段 |
| `CopyWithConvertersStrict` | **拷贝**，覆盖目标字段 | 整对象替换 / 重置：需要把字段显式置为 `false`、`0`、`""` |

注意：`CopyWithConverters` "忽略零值" 是既有语义（向后兼容，行为未变）。它带来一个常见坑：调用方**无法通过它把布尔字段置为 `false`、把计数置为 `0`、把字符串置为空**。需要这些能力时请使用 `CopyWithConvertersStrict`。

### CopyWithOptions / WithIgnoreEmpty / WithConverters

需要更细粒度控制时使用 `CopyWithOptions`：

```go
// CopyOption 用于配置 CopyWithOptions 的拷贝行为.
type CopyOption func(*copyOptions)

// WithIgnoreEmpty 设置是否忽略源中的零值字段（false / 0 / "" / nil）.
// 默认值为 true，与 CopyWithConverters 的既有语义保持一致；传入 false 时零值也会被拷贝.
func WithIgnoreEmpty(ignore bool) CopyOption

// WithConverters 追加自定义转换器.
// 注意：若自定义转换器与默认转换器（TypeConverters）注册的 (SrcType, DstType) 组合重复，
// 默认转换器会覆盖自定义转换器（与 CopyWithCustomConverters 的既有行为一致）.
func WithConverters(converters ...copier.TypeConverter) CopyOption

// CopyWithOptions 使用默认转换器做深拷贝，并允许通过 CopyOption 显式控制拷贝行为.
// 不传任何选项时，行为与 CopyWithConverters 完全一致（忽略零值）.
func CopyWithOptions(to any, from any, opts ...CopyOption) error
```

`CopyWithConvertersStrict(to, from)` 等价于 `CopyWithOptions(to, from, WithIgnoreEmpty(false))`。

### CopyWithCustomConverters

```go
// CopyWithCustomConverters 使用自定义转换器（在默认转换器之前注册）.
// to: 目标结构体指针; from: 源结构体指针；customConverters 自定义转换器.
func CopyWithCustomConverters(to any, from any, customConverters []copier.TypeConverter) error {
	return CopyWithOptions(to, from, WithConverters(customConverters...))
}
```

等价于 `CopyWithOptions(to, from, WithConverters(customConverters...))`，即在默认转换器的基础上追加自定义转换器，并且**不会修改调用方传入的 `customConverters` 底层数组**（旧实现直接 `append` 到入参 slice，当入参 `cap` 有余量时会污染调用方的底层数组）。

### CopyValueWithCustomConverters

```go
// CopyValueWithCustomConverters 将 from 的值根据 converters 中定义的转换规则
// 转换后赋值给 to 指向的变量。
func CopyValueWithCustomConverters(to any, from any, converters []copier.TypeConverter) error
```

单个值的转换赋值，参数要求与错误行为：

* `to` 必须是**非 nil 指针**，指向的变量类型必须等于某个转换器的 `DstType`；否则返回 error（不会 panic）；
* `from` 的类型必须等于某个转换器的 `SrcType`；`from` 为 `nil` 时无法匹配任何转换器；
* 转换器通过**显式声明的 `SrcType` / `DstType`** 匹配（二者都必须非 `nil`）；`Fn` 为 `nil` 的转换器不会被使用；
* 若转换器的 `Fn` 返回 `nil`，或返回值类型与 `DstType` 不兼容（`AssignableTo` 为 false），返回 error（不会 panic）；
* 未找到匹配的转换器时返回 `no converter found for <src> -> <dst>`。

注意 `DstType` 是**指针类型**时要格外小心，因为 `to` 必须指向一个该指针类型的变量：

```go
// TypeConverters 中 time.Time -> *timestamppb.Timestamp 的 DstType 是 *timestamppb.Timestamp，
// 所以 to 要指向 *timestamppb.Timestamp 变量（即 **timestamppb.Timestamp）
var ts *timestamppb.Timestamp
err := core.CopyValueWithCustomConverters(&ts, time.Now(), core.TypeConverters())

// 反向：*timestamppb.Timestamp -> time.Time，to 指向 time.Time 变量
var t time.Time
err = core.CopyValueWithCustomConverters(&t, ts, core.TypeConverters())
```

> 修复说明：旧实现在比较类型时写的是 `c.SrcType == srcType && c.DstType == dstType`。左侧是 `any`（动态类型是具体值类型），右侧是 `reflect.Type`（动态类型是 `*reflect.rtype`），两者**永远不相等**，因此该函数此前对任何输入都只会返回 "no converter found"（连内置转换器也匹配不上）。

### Copy

```go
// Copy 普通拷贝，不应用自定义转换，只做浅拷贝并赋值同名字段.
func Copy(to any, from any) error {
	return copier.Copy(to, from)
}
```

### 总结

* **`TypeConverters`**：内置转换器，提供 `time.Time ↔ *timestamppb.Timestamp`、`datatypes.JSON ↔ *structpb.Struct` 的互转；
* **`CopyWithConverters`**：在常规拷贝基础上，开启深拷贝、**忽略零值**，并使用默认转换器，适合部分更新（注意无法把字段置零）；
* **`CopyWithConvertersStrict`**：与 `CopyWithConverters` 相同，但**零值也会拷贝**，适合整对象替换/重置；
* **`CopyWithOptions`**：通过 `WithIgnoreEmpty` / `WithConverters` 显式控制零值与转换器，是上面几个函数的基础实现；
* **`CopyWithCustomConverters`**：在默认转换器基础上追加自定义转换器（不污染入参 slice）；
* **`CopyValueWithCustomConverters`**：对单个值做转换赋值，`to` 必须是非 nil 指针，转换器按显式声明的类型匹配；
* **`Copy`**：最简单的同名字段浅拷贝，不做类型转换，适合字段类型一致时使用。
