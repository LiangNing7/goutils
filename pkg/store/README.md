# store

> 定义了关于 Store 常用的一些函数。
>
> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/store
> ```

## where

> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/store/where
> ```

为了实现在查询数据库记录时，配置灵活的查询参数，大部分项目都可以通过 where 包来定制化查询条件。where 包提供了以下自定义查询条件：

```go
// Options 保存了GORM的Where查询条件的选项。  
type Options struct {  
    // Offset 定义了分页的起始位置。  
    // +optional  
    Offset int `json:"offset"`  
    // Limit 定义了返回结果的最大数量。  
    // +optional  
    Limit int `json:"limit"`  
    // Filters 包含用于过滤记录的键值对。  
    Filters map[any]any  
    // Clauses 包含要附加到查询中的自定义子句。  
    Clauses []clause.Expression  
}
// Options结构体中的字段最后会通过以下方式，来为*gorm.DB类型的实例添加查询条件：
// Where applies the filters and clauses to the given gorm.DB instance.
func (whr *Options) Where(db *gorm.DB) *gorm.DB {
    return db.Where(whr.Filters).Clauses(whr.Clauses...).Offset(whr.Offset).Limit(whr.Limit)
}
```

where 包通过配置 `where.Options` 结构体，来配置 GORM 的查询条件。Offset 字段用来配置查询记录时分页的起始位置，Limit 字段用来配置返回结果的最大数量，Filters 字段用来配置查询时过滤记录的键值对，Clauses 字段直接用来指定 `*gorm.DB` 的查询子句。where 包提供了以下三种方式，用来配置Options：

* 通过 NewWhere 函数构造；
* 通过便捷函数直接创建；
* 通过链式调用。

### 通过 NewWhere 函数钩子

where 包提供了 [NewWhere](https://github.com/LiangNing7/goutils/blob/master/pkg/store/where/where.go) 函数用来创建一个 `*Options` 类型的实例，在调用 NewWhere 函数时，通过传入不同的函数选项，来配置 `*Options` 结构体，具体函数如下【函数选项模式】：

```go
// Option 定义了一个函数类型，用于修改 Options。  
type Option func(*Options)  

// WithOffset 使用给定的 offset 值初始化 Options 的 Offset 字段。  
func WithOffset(offset int64) Option {  
    return func(whr *Options) {  
        if offset < 0 {  
            offset = 0  
        }  
        whr.Offset = int(offset)  
    }  
}  

// WithLimit 使用给定的 limit 值初始化 Options 的 Limit 字段。  
func WithLimit(limit int64) Option {  
    return func(whr *Options) {  
        if limit <= 0 {  
            limit = defaultLimit  
        }  
        whr.Limit = int(limit)  
    }  
}  

// WithPage 是一个糖函数，用于将 page 和 pageSize 转换为 Options 中的 limit 和 offset。  
// 此函数通常用于业务逻辑中以简化分页操作。  
func WithPage(page int, pageSize int) Option {  
    return func(whr *Options) {  
        if page == 0 {  
            page = 1  
        }  
        if pageSize == 0 {  
            pageSize = defaultLimit  
        }  

        whr.Offset = (page - 1) * pageSize  
        whr.Limit = pageSize  
    }  
}  

// WithFilter 使用给定的过滤条件初始化 Options 的 Filters 字段。  
func WithFilter(filter map[any]any) Option {  
    return func(whr *Options) {  
        whr.Filters = filter  
    }  
}  

// WithClauses 将指定的条件子句追加到 Options 的 Clauses 字段中。  
func WithClauses(conds ...clause.Expression) Option {  
    return func(whr *Options) {  
        whr.Clauses = append(whr.Clauses, conds...)  
    }  
}  

// NewWhere 构建一个新的 Options 对象，并应用所给定的 Option 修改。  
func NewWhere(opts ...Option) *Options {  
    whr := &Options{  
        Offset:  0,  
        Limit:   defaultLimit,  
        Filters: map[any]any{},  
        Clauses: make([]clause.Expression, 0),  
    }  

    for _, opt := range opts {  
        opt(whr) // 将每个 Option 应用于 Options。  
    }  

    return whr  
}
```

上述代码使用了函数选项模式来配置 `*Options` 结构体。函数选项模式是软件开发中高频使用的设计模式，该设计模式允许开发者根据需要为函数传递不同的选项参数，以定制函数的行为。

### 使用便捷函数直接创建

where 包还提供了一些便捷函数，用来快速创建一个指定了某类查询条件的 *Options 结构体实例，例如 O、L、P、C 等函数：

```go
// O 用于创建带有 offset 的新 Options 的便捷函数。  
func O(offset int) *Options {  
    return NewWhere().O(offset)  
}  

// L 用于创建带有 limit 的新 Options 的便捷函数。  
func L(limit int) *Options {  
    return NewWhere().L(limit)  
}  

// P 用于创建带有页码和每页大小的分页 Options 的便捷函数。  
func P(page int, pageSize int) *Options {  
    return NewWhere().P(page, pageSize)  
}

// C 用于创建带有条件的 Options 的便捷函数。
func C(conds ...clause.Expression) *Options {
	return NewWhere().C(conds...)
}
```

O、L、P、C 等便捷函数命名均以 Option 结构体中字段名的大写首字母命名。这种命名方式牺牲一点函数名可读性，但能有效减少代码折行的概率，有利于提高代码的可读性和简洁度。

### 通过链式调用

先调用 NewWhere 初始化空的 `*Options` 对象，然后通过链式调用设置分页、过滤条件或子句等内容，例如：

```go
opts := NewWhere().  
    O(10). // 设置 Offset 为 10  
    L(20). // 设置 Limit 为 20  
    F("name", "John", "status", "active"). // 添加过滤条件  
    C(clause.OrderBy{Columns: []clause.OrderByColumn{  
        {Column: clause.Column{Name: "created_at"}, Desc: true},  
    }}).  
    P(2, 10) // 设置分页：第2页，每页10条数据
```

链式调用（chaining）是一种通过方法返回自身实例的特性，实现连续调用的编程风格。链式调用具有简洁、灵活、语义化强等特点，特别适合对象的初始化、配置构建以及动态逻辑调整。它广泛应用于查询条件的组合、领域特定语言的设计等场景。通过链式调用，可以构建更加流畅的 API，提升代码可读性和开发体验，是很多现代框架与工具等普遍采用的设计模式。

## registry

> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/store/where
> ```

实现全局模型注册与自动迁移的机制，典型使用场景：

在你的项目启动时：

```go
import "github.com/LiangNing7/goutils/pkg/store/registry"
// 在各个 model 包的 init() 中注册模型
func init() {
    registry.Register(&User{})
    registry.Register(&Product{})
    // …更多模型
}

// 在应用启动时执行迁移
func main() {
    db := // … 初始化 GORM *gorm.DB
    if err := registry.Migrate(db); err != nil {
        log.Fatalf("自动迁移失败: %v", err)
    }
    // 启动 HTTP 服务等
}
```

### Register 函数注册模型

```go
// NewRegistry 创建并返回一个新的Registry实例  
func NewRegistry() *Registry {
	return &Registry{
		models: make([]interface{}, 0),
	}
}

func Register(model interface{}) {
	once.Do(func() {
		globalRegistry = NewRegistry()
	})
	globalRegistry.Register(model)
}

// Register 添加新的模型到Registry  
func (r *Registry) Register(model interface{}) {
	r.models = append(r.models, model)
}
```

### Migrate 函数自动迁移

```go
func Migrate(db *gorm.DB) error {
	if globalRegistry == nil {
		return nil
	}

	return globalRegistry.Migrate(db)
}

// Migrate 执行所有注册模型的迁移
func (r *Registry) Migrate(db *gorm.DB) error {
	for _, model := range r.models {
		if err := db.AutoMigrate(model); err != nil {
			return err
		}
	}
	return nil
}
```

## store

> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/store
> ```

`pkg/store` 包提供了一个通用的数据存储层，封装了：

数据库提供者（DBProvider）

* 定义了接口 `DBProvider`，用于根据 `context.Context` 和可选的查询条件（`where.Where`）获取一个 `*gorm.DB` 实例。

通用 Store 结构

* `Store[T any]` 是一个泛型结构，绑定一个具体数据模型 `T`，并持有：
  * `storage DBProvider`：实际的数据库连接提供者。
  * `logger Logger`：日志记录器（如未提供，默认使用空实现 `empty.NewLogger()`）。

常见 CRUD 方法

* `Create(ctx, obj)`：插入新记录。
* `Update(ctx, obj)`：保存（更新）已有记录。
* `Delete(ctx, opts)`：根据 `where.Options` 删除记录，忽略“记录不存在”错误。
* `Get(ctx, opts)`：根据条件查询单条记录。
* `List(ctx, opts)`：根据条件分页或排序查询多条记录，并返回总数。

可插拔的查询条件

* 在内部方法 `db(ctx, wheres...)` 中，将传入的一系列 `where.Where`（例如封装过滤、分页等）依次应用到 `*gorm.DB` 上，做到查询条件和存储操作解耦。
