package where

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// defaultLimit 是分页查询在调用方未指定 limit（或 limit 为 0）时使用的默认页大小.
	//
	// 注意：这里刻意使用一个正数而不是 -1. 在 gorm 中 Limit(-1) 表示"取消 LIMIT"，
	// 会让查询返回全表数据（DoS 与数据面风险）。若确实需要返回全表，必须显式调用
	// WithoutLimit()/WithUnlimited()，或沿用旧语义传入负数的 limit。
	defaultLimit = 20

	// unlimitedLimit 是显式取消 LIMIT 时使用的哨兵值，gorm 会忽略负数的 LIMIT.
	unlimitedLimit = -1
)

// ErrOddKeyValues 表示 F/FMust 收到的键值对参数个数为奇数，无法两两配对.
var ErrOddKeyValues = errors.New("where: key-value pairs must be even")

// normalizeLimit 把调用方传入的 limit 归一化成最终生效的 Limit 值：
//   - limit > 0：直接使用该值；
//   - limit == 0：使用 defaultLimit（默认页大小），避免意外的全表返回；
//   - limit < 0：返回 unlimitedLimit，显式取消 LIMIT（兼容旧的 L(-1) 语义）。
func normalizeLimit(limit int) int {
	switch {
	case limit > 0:
		return limit
	case limit == 0:
		return defaultLimit
	default:
		return unlimitedLimit
	}
}

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

// Options 定义了查询条件的集合，最终会被应用到 *gorm.DB 上.
type Options struct {
	// Offset 分页查询的起始位置(偏移量).
	Offset int `json:"offset"`
	// Limit 分页查询的最大记录数；<= 0 的语义由 normalizeLimit 统一处理，
	// 负数表示显式取消 LIMIT（返回全部记录）.
	Limit int `json:"limit"`
	// Filters 存放键值对过滤条件.
	Filters map[any]any
	// Clauses 存放自定义的 SQL 子句.
	Clauses []clause.Expression
	// Queries 存放多个额外的查询条件.
	Queries []Query

	// err 记录链式配置过程中产生的错误（例如 F 收到奇数个参数），
	// 以便调用方通过 Err() 感知，而不是被静默丢弃.
	err error
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

// WithLimit 创建一个设置 Limit 的 Option；若 limit == 0，则使用 defaultLimit，
// 若 limit < 0，则显式取消 LIMIT（返回全部记录）.
func WithLimit(limit int64) Option {
	return func(whr *Options) {
		whr.Limit = normalizeLimit(int(limit))
	}
}

// WithPage 将页码和每页数量转换为 Offset 和 Limit 的便捷方法；
// pageSize == 0 时使用 defaultLimit，pageSize < 0 时取消 LIMIT.
func WithPage(page int, pageSize int) Option {
	return func(whr *Options) {
		if page < 1 {
			page = 1
		}
		pageSize = normalizeLimit(pageSize)
		if pageSize == unlimitedLimit {
			whr.Offset = 0
			whr.Limit = unlimitedLimit
			return
		}
		whr.Offset = (page - 1) * pageSize
		whr.Limit = pageSize
	}
}

// WithUnlimited 创建一个显式取消 LIMIT 的 Option，查询将返回全部匹配记录.
//
// 该选项用于替代旧的 "limit <= 0 即取消限制" 的隐式行为，请仅在确实需要全量数据时使用.
func WithUnlimited() Option {
	return func(whr *Options) {
		whr.Limit = unlimitedLimit
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
// 默认 Limit 为 defaultLimit（不再是无限制的全表返回）.
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
// limit == 0 使用 defaultLimit，limit < 0 显式取消 LIMIT（返回全部记录）.
func (whr *Options) L(limit int) *Options {
	whr.Limit = normalizeLimit(limit)
	return whr
}

// WithoutLimit 显式取消 LIMIT，查询将返回全部匹配记录，并返回自身以支持链式调用.
//
// 该方法用于替代旧的 "limit <= 0 即取消限制" 的隐式行为，请仅在确实需要全量数据时使用.
func (whr *Options) WithoutLimit() *Options {
	whr.Limit = unlimitedLimit
	return whr
}

// P 根据页码和页大小设置分页参数，支持链式调用.
// pageSize == 0 使用 defaultLimit，pageSize < 0 取消 LIMIT.
func (whr *Options) P(page int, pageSize int) *Options {
	if page < 1 {
		page = 1
	}
	pageSize = normalizeLimit(pageSize)
	if pageSize == unlimitedLimit {
		whr.Offset = 0
		whr.Limit = unlimitedLimit
		return whr
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

// F 向 Filters 中批量添加键值对.
//
// 当参数个数为奇数时，会应用所有已配对的键值对（保证租户等关键过滤条件不被丢弃），
// 同时通过 err 记录错误，调用方可使用 Err() 感知；若需要在调用点强制处理错误，请使用 FMust.
func (whr *Options) F(kvs ...any) *Options {
	if len(kvs)%2 != 0 {
		whr.err = fmt.Errorf("%w: got %d args", ErrOddKeyValues, len(kvs))
		// 不再直接返回：继续应用已配对的键值对，避免连带丢失租户过滤条件.
	}
	pairs := len(kvs) - len(kvs)%2
	for i := 0; i < pairs; i += 2 {
		key := kvs[i]
		value := kvs[i+1]
		whr.Filters[key] = value
	}
	return whr
}

// FMust 与 F 行为一致，但额外返回 error，便于调用方强制处理奇数参数等错误.
func (whr *Options) FMust(kvs ...any) (*Options, error) {
	whr.F(kvs...)
	if whr.err != nil {
		return whr, whr.err
	}
	return whr, nil
}

// Err 返回链式配置过程中记录的第一个错误（例如 F 收到奇数个参数），没有错误时返回 nil.
func (whr *Options) Err() error {
	return whr.err
}

// Where 将 Filters、Clauses、Offset、Limit 等应用到传入的 *gorm.DB 并返回构造后的 *gorm.DB.
//
// 该方法不会修改 receiver：由 Queries 构建出的子句只写入局部变量，
// 因此同一个 *Options 可以被安全地重复、并发地应用到多个 *gorm.DB 上.
func (whr *Options) Where(db *gorm.DB) *gorm.DB {
	all := make([]clause.Expression, 0, len(whr.Queries)+len(whr.Clauses))
	// BuildCondition 会将单条 Query 转换成 clause.Expression.
	for _, query := range whr.Queries {
		all = append(all, db.Statement.BuildCondition(query.Query, query.Args...)...)
	}
	// 按顺序应用 Filters、Clauses、Offset、Limit.
	return db.
		Where(whr.Filters).
		Clauses(append(all, whr.Clauses...)...).
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
// 未注册租户或 ValueFunc 为 nil 时不会 panic，直接返回不带租户过滤的 Options，
// 与方法 T 的行为保持一致.
func T(ctx context.Context) *Options {
	return NewWhere().T(ctx)
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
