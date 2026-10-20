package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/LiangNing7/goutils/pkg/store/where"
)

type storeUser struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

// fakeProvider 是一个只返回基础 *gorm.DB 的 DBProvider，where 条件由 Store 自身应用.
type fakeProvider struct{ db *gorm.DB }

func (p *fakeProvider) DB(ctx context.Context, wheres ...where.Where) *gorm.DB {
	return p.db.WithContext(ctx)
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&storeUser{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func newTestStore(t *testing.T, db *gorm.DB) *Store[storeUser] {
	t.Helper()
	return NewStore[storeUser](&fakeProvider{db: db}, nil)
}

func countUsers(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&storeUser{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestGetWithTypedNilWhere 回归 typed-nil：传入 (*where.Options)(nil) 不能 panic.
func TestGetWithTypedNilWhere(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)
	if err := db.Create(&storeUser{Name: "alice"}).Error; err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(context.Background(), (*where.Options)(nil))
	if err != nil {
		t.Fatalf("Get with typed-nil where 不应报错: %v", err)
	}
	if got.Name != "alice" {
		t.Fatalf("got %+v", got)
	}
}

// TestListWithTypedNilWhere 回归 typed-nil：List 传入 (*where.Options)(nil) 不能 panic.
func TestListWithTypedNilWhere(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)
	for _, n := range []string{"a", "b", "c"} {
		if err := db.Create(&storeUser{Name: n}).Error; err != nil {
			t.Fatal(err)
		}
	}

	count, rows, err := s.List(context.Background(), (*where.Options)(nil))
	if err != nil {
		t.Fatalf("List with typed-nil where 不应报错: %v", err)
	}
	if count != 3 || len(rows) != 3 {
		t.Fatalf("count=%d len(rows)=%d, want 3/3", count, len(rows))
	}
}

// TestDeleteWithTypedNilWhere 回归 typed-nil：Delete 传入 (*where.Options)(nil) 不能 panic。
// typed-nil 会被跳过，于是变成不带 WHERE 条件的 DELETE，被 gorm 的全局更新保护拦下（ErrMissingWhereClause）。
func TestDeleteWithTypedNilWhere(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)
	if err := db.Create(&storeUser{Name: "alice"}).Error; err != nil {
		t.Fatal(err)
	}

	err := s.Delete(context.Background(), (*where.Options)(nil))
	if !errors.Is(err, gorm.ErrMissingWhereClause) {
		t.Fatalf("err = %v, want gorm.ErrMissingWhereClause", err)
	}
	if n := countUsers(t, db); n != 1 {
		t.Fatalf("剩余记录数 = %d, want 1（无 WHERE 的删除被拒绝）", n)
	}
}

// TestDBNilWhereInterface 回归未经装箱的 nil 接口值.
func TestDBNilWhereInterface(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)

	// 不应 panic.
	_ = s.db(context.Background(), nil)
	_ = s.db(context.Background(), (*where.Options)(nil))
}

// TestGetWithRealWhere 正常的 where 条件仍然生效.
func TestGetWithRealWhere(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)
	for _, n := range []string{"alice", "bob"} {
		if err := db.Create(&storeUser{Name: n}).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Get(context.Background(), where.NewWhere().F("name", "bob"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "bob" {
		t.Fatalf("got %+v", got)
	}
}

// TestUpdateWithoutPrimaryKey 回归 Update 用 Save 导致空主键 INSERT 出重复行.
func TestUpdateWithoutPrimaryKey(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)
	if err := db.Create(&storeUser{Name: "alice"}).Error; err != nil {
		t.Fatal(err)
	}

	err := s.Update(context.Background(), &storeUser{Name: "alice-updated"})
	if !errors.Is(err, ErrMissingPrimaryKey) {
		t.Fatalf("err = %v, want ErrMissingPrimaryKey", err)
	}
	if n := countUsers(t, db); n != 1 {
		t.Fatalf("空主键 Update 不应插入新行，剩余记录数 = %d, want 1", n)
	}
}

// TestUpdateWithPrimaryKey 主键存在时正常更新且不新增行.
func TestUpdateWithPrimaryKey(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)
	obj := &storeUser{Name: "alice"}
	if err := db.Create(obj).Error; err != nil {
		t.Fatal(err)
	}

	obj.Name = "alice-updated"
	if err := s.Update(context.Background(), obj); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if n := countUsers(t, db); n != 1 {
		t.Fatalf("剩余记录数 = %d, want 1", n)
	}
	var got storeUser
	if err := db.First(&got, obj.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "alice-updated" {
		t.Fatalf("name = %q, want alice-updated", got.Name)
	}
}

// TestCreateAndList 基本的 Create/List 行为.
func TestCreateAndList(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)

	for _, n := range []string{"a", "b", "c"} {
		if err := s.Create(context.Background(), &storeUser{Name: n}); err != nil {
			t.Fatal(err)
		}
	}

	count, rows, err := s.List(context.Background(), where.NewWhere().L(2))
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (Limit 生效)", len(rows))
	}
}

// TestListDefaultLimitBounds 未显式指定 limit 时，List 必须被默认页大小限制住（不再全表返回）.
func TestListDefaultLimitBounds(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)

	for i := 0; i < 25; i++ {
		if err := db.Create(&storeUser{Name: fmt.Sprintf("u%02d", i)}).Error; err != nil {
			t.Fatal(err)
		}
	}

	count, rows, err := s.List(context.Background(), where.NewWhere())
	if err != nil {
		t.Fatal(err)
	}
	if count != 25 {
		t.Fatalf("count = %d, want 25", count)
	}
	if len(rows) != 20 {
		t.Fatalf("len(rows) = %d, want 20 (默认页大小生效)", len(rows))
	}
}

// TestDeleteWithFilterAndDefaultLimit 默认 Limit 不应被带进 DELETE 语句（sqlite 不支持 DELETE ... LIMIT）.
func TestDeleteWithFilterAndDefaultLimit(t *testing.T) {
	db := newTestDB(t)
	s := newTestStore(t, db)

	for _, n := range []string{"a", "b"} {
		if err := db.Create(&storeUser{Name: n}).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Delete(context.Background(), where.NewWhere().F("name", "a")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var names []string
	if err := db.Model(&storeUser{}).Pluck("name", &names).Error; err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "b" {
		t.Fatalf("删除后剩余记录 = %v, want [b]", names)
	}
}
