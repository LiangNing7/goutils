package store

import (
	"context"
	"errors"
	"reflect"

	"gorm.io/gorm"

	"github.com/LiangNing7/goutils/pkg/store/logger/empty"
	"github.com/LiangNing7/goutils/pkg/store/where"
)

// ErrMissingPrimaryKey 表示 Update 时对象未设置主键。
// 缺少主键时 gorm 的 Save 会退化为 INSERT 从而产生重复行，因此这里直接拒绝执行。
var ErrMissingPrimaryKey = errors.New("store: update requires a non-zero primary key")

// isNilWhere 判断 where.Where 是否为 nil，包含 "typed-nil"（例如 (*where.Options)(nil) 装箱成接口后 != nil）的情况。
func isNilWhere(whr where.Where) bool {
	if whr == nil {
		return true
	}
	v := reflect.ValueOf(whr)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}

// DBProvider defines an interface for providing a database connection.
type DBProvider interface {
	// DB returns the database instance for the given context.
	DB(ctx context.Context, wheres ...where.Where) *gorm.DB
}

// Option defines a function type for configuring the Store.
type Option[T any] func(*Store[T])

// Store represents a generic data store with logging capabilities.
type Store[T any] struct {
	logger  Logger
	storage DBProvider
}

// WithLogger returns an Option function that sets the provided Logger to the Store for logging purposes.
func WithLogger[T any](logger Logger) Option[T] {
	return func(s *Store[T]) {
		s.logger = logger
	}
}

// NewStore creates a new instance of Store with the provided DBProvider.
func NewStore[T any](storage DBProvider, logger Logger) *Store[T] {
	if logger == nil {
		logger = empty.NewLogger()
	}

	return &Store[T]{
		logger:  logger,
		storage: storage,
	}
}

// db retrieves the database instance and applies the provided where conditions.
// 传入 nil 或 typed-nil 的 where.Where 会被跳过，避免解引用 nil receiver 导致 panic.
func (s *Store[T]) db(ctx context.Context, wheres ...where.Where) *gorm.DB {
	dbInstance := s.storage.DB(ctx)
	for _, whr := range wheres {
		if !isNilWhere(whr) {
			dbInstance = whr.Where(dbInstance)
		}
	}
	return dbInstance
}

// Create inserts a new object into the database.
func (s *Store[T]) Create(ctx context.Context, obj *T) error {
	if err := s.db(ctx).Create(obj).Error; err != nil {
		s.logger.Error(ctx, err, "Failed to insert object into database", "object", obj)
		return err
	}
	return nil
}

// Update modifies an existing object in the database.
//
// 与旧实现直接调用 gorm 的 Save 不同，Update 会先校验 obj 是否设置了主键：
// 缺少主键时 gorm 的 Save 会退化为 INSERT，从而插入重复行，因此这里直接返回
// ErrMissingPrimaryKey 而不是静默地写入新行。设置了主键的行为与 Save 保持一致。
func (s *Store[T]) Update(ctx context.Context, obj *T) error {
	db := s.db(ctx)
	if err := checkPrimaryKey(db, ctx, obj); err != nil {
		s.logger.Error(ctx, err, "Failed to update object in database", "object", obj)
		return err
	}
	if err := db.Save(obj).Error; err != nil {
		s.logger.Error(ctx, err, "Failed to update object in database", "object", obj)
		return err
	}
	return nil
}

// checkPrimaryKey 校验 obj 的主键是否已设置（非零值）。未定义主键或主键为零值时返回 ErrMissingPrimaryKey。
func checkPrimaryKey(db *gorm.DB, ctx context.Context, obj any) error {
	if obj == nil || reflect.ValueOf(obj).Kind() != reflect.Ptr || reflect.ValueOf(obj).IsNil() {
		return ErrMissingPrimaryKey
	}
	stmt := &gorm.Statement{DB: db, Context: ctx}
	if err := stmt.Parse(obj); err != nil {
		return err
	}
	if stmt.Schema == nil || len(stmt.Schema.PrimaryFields) == 0 {
		return ErrMissingPrimaryKey
	}
	for _, field := range stmt.Schema.PrimaryFields {
		if _, isZero := field.ValueOf(ctx, reflect.ValueOf(obj)); isZero {
			return ErrMissingPrimaryKey
		}
	}
	return nil
}

// Delete removes an object from the database based on the provided where options.
func (s *Store[T]) Delete(ctx context.Context, opts *where.Options) error {
	err := s.db(ctx, opts).Delete(new(T)).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.logger.Error(ctx, err, "Failed to delete object from database", "conditions", opts)
		return err
	}
	return nil
}

// Get retrieves a single object from the database based on the provided where options.
func (s *Store[T]) Get(ctx context.Context, opts *where.Options) (*T, error) {
	var obj T
	if err := s.db(ctx, opts).First(&obj).Error; err != nil {
		s.logger.Error(ctx, err, "Failed to retrieve object from database", "conditions", opts)
		return nil, err
	}
	return &obj, nil
}

// List retrieves a list of objects from the database based on the provided where options.
func (s *Store[T]) List(ctx context.Context, opts *where.Options) (count int64, ret []*T, err error) {
	err = s.db(ctx, opts).Order("id desc").Find(&ret).Offset(-1).Limit(-1).Count(&count).Error
	if err != nil {
		s.logger.Error(ctx, err, "Failed to list objects from database", "conditions", opts)
	}
	return
}
