package where

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testUser 仅用于生成 SQL，验证 Limit/Offset/过滤条件是否被正确应用.
type testUser struct {
	ID   uint `gorm:"primaryKey"`
	Name string
	Age  int
}

// newDryRunDB 构造一个纯内存 sqlite 的 DryRun gorm.DB，仅用于断言生成的 SQL.
func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	return db
}

// sqlFor 返回将 opts 应用到 testUser 的 Find 查询后生成的 SQL.
func sqlFor(t *testing.T, opts *Options) string {
	t.Helper()
	db := newDryRunDB(t)
	tx := db.Session(&gorm.Session{DryRun: true}).Model(&testUser{})
	tx = opts.Where(tx)
	tx.Find(&[]*testUser{})
	return tx.Statement.SQL.String()
}

// TestDefaultLimitIsBounded 回归 defaultLimit=-1 导致 limit<=0 时全表返回的问题.
func TestDefaultLimitIsBounded(t *testing.T) {
	cases := map[string]*Options{
		"NewWhere()":     NewWhere(),
		"L(0)":           L(0),
		"P(1, 0)":        P(1, 0),
		"P(0, 0)":        P(0, 0),
		"O(0)":           O(0),
		"WithLimit(0)":   NewWhere(WithLimit(0)),
		"WithPage(1, 0)": NewWhere(WithPage(1, 0)),
		"WithPage(0, 0)": NewWhere(WithPage(0, 0)),
	}

	for name, opts := range cases {
		if opts.Limit != defaultLimit {
			t.Errorf("%s: Limit = %d, want defaultLimit %d", name, opts.Limit, defaultLimit)
		}
		if opts.Limit <= 0 {
			t.Errorf("%s: Limit 必须为正数，实际 %d（-1 会被 gorm 解释为取消 LIMIT）", name, opts.Limit)
		}
	}

	// 显式指定 limit/pageSize 时仍按调用方要求生效.
	if got := NewWhere(WithPage(2, 10)); got.Limit != 10 || got.Offset != 10 {
		t.Errorf("WithPage(2,10): Limit=%d Offset=%d, want 10/10", got.Limit, got.Offset)
	}
	if got := P(2, 10); got.Limit != 10 || got.Offset != 10 {
		t.Errorf("P(2,10): Limit=%d Offset=%d, want 10/10", got.Limit, got.Offset)
	}
	if got := L(5); got.Limit != 5 {
		t.Errorf("L(5): Limit=%d, want 5", got.Limit)
	}

	// SQL 层面确认 LIMIT 存在且等于默认页大小.
	sql := sqlFor(t, NewWhere())
	if !strings.Contains(sql, "LIMIT 20") {
		t.Fatalf("NewWhere() 生成的 SQL 未包含 LIMIT 20: %q", sql)
	}
}

// TestExplicitUnlimited 显式取消限制的路径必须仍然可用（保留 L(-1) 的旧语义）.
func TestExplicitUnlimited(t *testing.T) {
	cases := map[string]*Options{
		"WithoutLimit()":  NewWhere().WithoutLimit(),
		"WithUnlimited()": NewWhere(WithUnlimited()),
		"L(-1) 旧语义":       L(-1),
		"WithLimit(-1)":   NewWhere(WithLimit(-1)),
	}
	for name, opts := range cases {
		if opts.Limit != -1 {
			t.Errorf("%s: Limit = %d, want -1 (取消 LIMIT)", name, opts.Limit)
		}
	}

	for name, opts := range cases {
		sql := sqlFor(t, opts)
		if strings.Contains(sql, "LIMIT") {
			t.Errorf("%s: 显式取消限制后 SQL 仍包含 LIMIT: %q", name, sql)
		}
	}
}

// TestWhereIsIdempotent 同一个 *Options 连续调用 Where 不能累积子句.
func TestWhereIsIdempotent(t *testing.T) {
	opts := NewWhere().Q("name = ?", "alice").F("age", 18)

	sql1 := sqlFor(t, opts)
	sql2 := sqlFor(t, opts)

	if sql1 != sql2 {
		t.Fatalf("Where 非幂等:\n第一次: %q\n第二次: %q", sql1, sql2)
	}
	if len(opts.Clauses) != 0 {
		t.Fatalf("Where 不应修改 receiver 的 Clauses，实际长度 %d", len(opts.Clauses))
	}
	if strings.Count(sql1, "name = ?") != 1 {
		t.Fatalf("查询条件在 SQL 中出现次数 != 1: %q", sql1)
	}
}

// TestWhereConcurrentReuse 并发复用同一个 *Options 不应产生 data race（需 -race 运行）.
func TestWhereConcurrentReuse(t *testing.T) {
	opts := NewWhere().Q("name = ?", "alice").F("age", 18)
	db := newDryRunDB(t)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx := db.Session(&gorm.Session{DryRun: true}).Model(&testUser{})
			_ = opts.Where(tx).Find(&[]*testUser{})
		}()
	}
	wg.Wait()
}

// TestTenantUnregisteredNoPanic 未注册租户时，包级 T 与方法 T 都不应 panic，行为一致.
func TestTenantUnregisteredNoPanic(t *testing.T) {
	RegisterTenant("", nil) // 重置为未注册状态
	defer RegisterTenant("", nil)

	pkgOpts := T(context.Background())
	if len(pkgOpts.Filters) != 0 {
		t.Fatalf("未注册租户时包级 T 不应产生过滤条件: %v", pkgOpts.Filters)
	}

	methodOpts := NewWhere().T(context.Background())
	if len(methodOpts.Filters) != 0 {
		t.Fatalf("未注册租户时方法 T 不应产生过滤条件: %v", methodOpts.Filters)
	}
}

// TestTenantRegistered 注册租户后，包级 T 与方法 T 都要生效.
func TestTenantRegistered(t *testing.T) {
	RegisterTenant("tenant_id", func(ctx context.Context) string { return "t-1" })
	defer RegisterTenant("", nil)

	pkgOpts := T(context.Background())
	if pkgOpts.Filters["tenant_id"] != "t-1" {
		t.Fatalf("包级 T 未注入租户过滤: %v", pkgOpts.Filters)
	}

	methodOpts := NewWhere().T(context.Background())
	if methodOpts.Filters["tenant_id"] != "t-1" {
		t.Fatalf("方法 T 未注入租户过滤: %v", methodOpts.Filters)
	}
}

// TestF_OddArgsNotSilent F 参数个数为奇数时不应静默丢弃已配对的条件，并且要能被上层感知.
func TestF_OddArgsNotSilent(t *testing.T) {
	opts := NewWhere().F("tenant_id", "t-1", "dangling")
	if err := opts.Err(); err == nil {
		t.Fatal("F 传入奇数个参数必须记录错误，实际为 nil")
	}
	if opts.Filters["tenant_id"] != "t-1" {
		t.Fatalf("已配对的租户过滤条件不应被丢弃: %v", opts.Filters)
	}

	ok := NewWhere().F("a", 1)
	if err := ok.Err(); err != nil {
		t.Fatalf("偶数参数不应报错: %v", err)
	}
}

// TestFMust FMust 直接返回 error，便于调用方强制处理.
func TestFMust(t *testing.T) {
	if _, err := NewWhere().FMust("a"); err == nil {
		t.Fatal("FMust 奇数参数应返回错误")
	}
	opts, err := NewWhere().FMust("a", 1)
	if err != nil {
		t.Fatalf("FMust 偶数参数不应报错: %v", err)
	}
	if opts.Filters["a"] != 1 {
		t.Fatalf("FMust 未写入过滤条件: %v", opts.Filters)
	}
}
