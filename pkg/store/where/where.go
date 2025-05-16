package where

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// defaultLimit 定义了分页查询时的默认最大记录数.
	defaultLimit = -1
)

// Tenant 表示一个租户，包含用于获取其值的键和值获取函数.
type Tenant struct {
	Key       string                           // 与 tenant 关联的键
	ValueFunc func(ctx context.Context) string // 根据上下文获取 tenant's value
}

// Where 定义了一个接口，表示能够对 GORM 查询进行修改的类型.
type Where interface {
	Where(db *gorm.DB) *gorm.DB
}

// Query 表示一条查询条件及其参数.
type Query struct {
	// Query 存放传递给 GORM Where 的查询条件，可以是 string、map、struct 等.
	Query any

	// Args 存放用于替换查询条件中占位符的参数列表.
	Args []any
}

// Option 定义了一个函数类型，用于修改 Options 对象.
type Option func(*Options)

// Options 定义了一个函数类型，用于修改 Options 对象.
type Options struct {
	// Offset 分页查询的起始位置(偏移量).
	Offset int `json:"offset"`
	// Limit 分页查询的最大记录数.
	Limit int `json:"limit"`
	// Filters 存放键值对过滤条件.
	Filters map[any]any
	// Clauses 存放自定义的 SQL 子句.
	Clauses []clause.Expression
	// Queries 存放多个额外的查询条件.
	Queries []Query
}

// registeredTenant 持有全局注册的 Tenant 信息.
var registeredTenant Tenant

// WithOffset 创建一个设置 offset 的 Option，若 offset < 0，则修正为 0.
func WithOffset(offset int64) Option {
	return func(whr *Options) {
		if offset < 0 {
			offset = 0
		}
		whr.Offset = int(offset)
	}
}

// WithLimit 创建一个设置 Limit 的 Option；若 limit <= 0，则使用 defaultLimit.
func WithLimit(limit int64) Option {
	return func(whr *Options) {
		if limit <= 0 {
			limit = defaultLimit
		}
		whr.Limit = int(limit)
	}
}

// WithPage 将页码和每页数量转换为 Offset 和 Limit 的便捷方法.
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

// WithFilter 创建一个设置 Filters 的 Option.
func WithFilter(filter map[any]any) Option {
	return func(whr *Options) {
		whr.Filters = filter
	}
}

// WithClauses 创建一个添加自定义子句的 Option.
func WithClauses(conds ...clause.Expression) Option {
	return func(whr *Options) {
		whr.Clauses = append(whr.Clauses, conds...)
	}
}

// WithQuery 创建一个添加单条查询条件（Query）的 Option.
func WithQuery(query any, args ...any) Option {
	return func(whr *Options) {
		whr.Queries = append(whr.Queries, Query{Query: query, Args: args})
	}
}

// NewWhere 根据传入的 Option 构造并返回一个初始化号的 Options 实例.
func NewWhere(opts ...Option) *Options {
	whr := &Options{
		Offset:  0,
		Limit:   defaultLimit,
		Filters: map[any]any{},
		Clauses: []clause.Expression{},
	}
	for _, opt := range opts {
		opt(whr)
	}
	return whr
}

// O 设置 Offset 并返回自身，支持链式调用.
func (whr *Options) O(offset int) *Options {
	if offset < 0 {
		offset = 0
	}
	whr.Offset = offset
	return whr
}

// L 设置 Limit 并返回自身，支持链式调用.
func (whr *Options) L(limit int) *Options {
	if limit <= 0 {
		limit = defaultLimit
	}
	whr.Limit = limit
	return whr
}

// P 根据页码和页大小设置分页参数，支持链式调用.
func (whr *Options) P(page int, pageSize int) *Options {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultLimit
	}
	whr.Offset = (page - 1) * pageSize
	whr.Limit = pageSize
	return whr
}

// C 添加自定义子句到 Clauses 并返回自身.
func (whr *Options) C(conds ...clause.Expression) *Options {
	whr.Clauses = append(whr.Clauses, conds...)
	return whr
}

// Q 添加一条 Query 条件到 Queries 并返回自身.
func (whr *Options) Q(query any, args ...any) *Options {
	whr.Queries = append(whr.Queries, Query{Query: query, Args: args})
	return whr
}

// T 根据全局注册的 Tenant 信息，向 Filters 中添加 Tenant 键值对.
func (whr *Options) T(ctx context.Context) *Options {
	if registeredTenant.Key != "" && registeredTenant.ValueFunc != nil {
		whr.F(registeredTenant.Key, registeredTenant.ValueFunc(ctx))
	}
	return whr
}

// F 向 Filters 中批量添加键值对；若参数个数为奇数则忽略.
func (whr *Options) F(kvs ...any) *Options {
	if len(kvs)%2 != 0 {
		// 键值对数量不匹配，直接返回不做修改.
		return whr
	}
	for i := 0; i < len(kvs); i += 2 {
		key := kvs[i]
		value := kvs[i+1]
		whr.Filters[key] = value
	}
	return whr
}

// Where 将 Filters、Clauses、Offset、Limit 等应用到传入的 *gorm.DB 并返回构造后的 *gorm.DB.
func (whr *Options) Where(db *gorm.DB) *gorm.DB {
	for _, query := range whr.Queries {
		// BuildCondition 会将单条 Query 转换成 clause.Expression.
		conds := db.Statement.BuildCondition(query.Query, query.Args...)
		whr.Clauses = append(whr.Clauses, conds...)
	}
	// 按顺序应用 Filters、Clauses、Offset、Limit.
	return db.
		Where(whr.Filters).
		Clauses(whr.Clauses...).
		Offset(whr.Offset).
		Limit(whr.Limit)
}

// 下面是一组便捷函数，直接返回应用了对应参数的 Options.

// O 是创建带 Offset 的 Options 的简写.
func O(offset int) *Options {
	return NewWhere().O(offset)
}

// L 是创建带 Limit 的 Options 的简写.
func L(limit int) *Options {
	return NewWhere().L(limit)
}

// P 是创建带分页参数的 Options 的简写.
func P(page int, pageSize int) *Options {
	return NewWhere().P(page, pageSize)
}

// C 是创建带自定义子句的 Options 的简写.
func C(conds ...clause.Expression) *Options {
	return NewWhere().C(conds...)
}

// T 是创建带租户过滤的 Options 的简写.
func T(ctx context.Context) *Options {
	return NewWhere().F(registeredTenant.Key, registeredTenant.ValueFunc(ctx))
}

// F 是创建带 Filters 的 Options 的简写.
func F(kvs ...any) *Options {
	return NewWhere().F(kvs...)
}

// RegisterTenant 注册一个租户，设置全局的租户 Key 和 ValueFunc.
func RegisterTenant(key string, valueFunc func(context.Context) string) {
	registeredTenant = Tenant{
		Key:       key,
		ValueFunc: valueFunc,
	}
}
