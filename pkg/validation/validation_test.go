package validation_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LiangNing7/goutils/pkg/db"
	"github.com/LiangNing7/goutils/pkg/options"
	"github.com/LiangNing7/goutils/pkg/validation"
)

// User 与其它包中的同名类型一起用于验证 registry key 的唯一性.
type User struct {
	Name string
}

// customValidator 注册的校验方法名必须形如 Validate+类型名.
type customValidator struct{}

func (customValidator) ValidateMySQLOptions(ctx context.Context, request *options.MySQLOptions) error {
	if request.Addr == "" {
		return errors.New("addr is required")
	}
	return nil
}

// TestValidator_UsesFullTypePath 覆盖 registry 以类型名作 key 导致跨包同名类型串用校验器的问题.
func TestValidator_UsesFullTypePath(t *testing.T) {
	v := validation.NewValidator(customValidator{})

	// 正例：注册的类型会被调用
	assert.Error(t, v.Validate(context.Background(), &options.MySQLOptions{}))

	err := v.Validate(context.Background(), &options.MySQLOptions{Addr: "127.0.0.1"})
	assert.NoError(t, err)

	// 反例：另一个包中同名的 MySQLOptions 不得复用上面的校验器（旧实现会 panic）
	assert.NotPanics(t, func() {
		err = v.Validate(context.Background(), &db.MySQLOptions{})
	})
	assert.NoError(t, err, "a same-named type from another package must not reuse the registered validator")

	// 反例：本包中的同名类型（User）不得复用（这里未注册 User，应直接返回 nil）
	assert.NotPanics(t, func() {
		err = v.Validate(context.Background(), &User{})
	})
	assert.NoError(t, err)
}

// TestValidator_InvalidRequest 覆盖 nil / 非指针入参不得 panic.
func TestValidator_InvalidRequest(t *testing.T) {
	v := validation.NewValidator(customValidator{})

	assert.NotPanics(t, func() {
		assert.NoError(t, v.Validate(context.Background(), nil))
	})
	assert.NotPanics(t, func() {
		assert.NoError(t, v.Validate(context.Background(), User{}))
	})
	assert.NotPanics(t, func() {
		assert.NoError(t, v.Validate(context.Background(), (*User)(nil)))
	})
	assert.NotPanics(t, func() {
		assert.NoError(t, v.Validate(context.Background(), 1))
	})
}

// TestNewValidator_NilCustomValidator 覆盖 nil 自定义校验器不得 panic.
func TestNewValidator_NilCustomValidator(t *testing.T) {
	var v *validation.Validator
	assert.NotPanics(t, func() { v = validation.NewValidator(nil) })
	assert.NotNil(t, v)
	assert.NoError(t, v.Validate(context.Background(), &options.MySQLOptions{Addr: "127.0.0.1"}))
}

// TestValidator_ConcurrentValidate 覆盖校验器实例的并发使用（配合 -race）.
func TestValidator_ConcurrentValidate(t *testing.T) {
	v := validation.NewValidator(customValidator{})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = v.Validate(context.Background(), &options.MySQLOptions{Addr: "127.0.0.1"})
				_ = v.Validate(context.Background(), &db.MySQLOptions{})
				_ = v.Validate(context.Background(), &options.MySQLOptions{})
			}
		}()
	}
	wg.Wait()
}

func TestValidRequired_Kinds(t *testing.T) {
	type req struct {
		Name  string
		Age   int
		Note  *string
		Tags  []string
		Attrs map[string]string
		Any   any
	}

	note := "note"

	tests := []struct {
		name    string
		req     *req
		fields  []string
		wantErr string
	}{
		{name: "string empty", req: &req{}, fields: []string{"Name"}, wantErr: "field Name must be provided"},
		{name: "string set", req: &req{Name: "n"}, fields: []string{"Name"}},
		{name: "int zero", req: &req{}, fields: []string{"Age"}, wantErr: "field Age must be provided"},
		{name: "int set", req: &req{Age: 1}, fields: []string{"Age"}},
		{name: "pointer nil", req: &req{}, fields: []string{"Note"}, wantErr: "field Note must be provided"},
		{name: "pointer set", req: &req{Note: &note}, fields: []string{"Note"}},
		{name: "slice nil", req: &req{}, fields: []string{"Tags"}, wantErr: "field Tags must be provided"},
		{name: "slice empty but not nil", req: &req{Tags: []string{}}, fields: []string{"Tags"}},
		{name: "map nil", req: &req{}, fields: []string{"Attrs"}, wantErr: "field Attrs must be provided"},
		{name: "map empty but not nil", req: &req{Attrs: map[string]string{}}, fields: []string{"Attrs"}},
		{name: "interface nil", req: &req{}, fields: []string{"Any"}, wantErr: "field Any must be provided"},
		{name: "interface set", req: &req{Any: 0}, fields: []string{"Any"}},
		{name: "unknown field", req: &req{}, fields: []string{"Missing"}, wantErr: "field Missing does not exist in struct"},
		{
			name:   "multiple fields",
			req:    &req{Name: "n", Age: 1, Note: &note, Tags: []string{}, Attrs: map[string]string{}, Any: 0},
			fields: []string{"Name", "Age", "Note", "Tags", "Attrs", "Any"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			assert.NotPanics(t, func() { err = validation.ValidRequired(tt.req, tt.fields...) })
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.wantErr)
		})
	}

	// 值传递的结构体同样可用
	assert.NoError(t, validation.ValidRequired(req{Name: "n"}, "Name"))
	assert.Error(t, validation.ValidRequired(req{}, "Name"))
}

func TestValidRequired_InvalidInput(t *testing.T) {
	type req struct {
		Name string
	}

	note := "n"
	ptr := &req{Name: note}

	t.Run("nil input", func(t *testing.T) {
		var err error
		assert.NotPanics(t, func() { err = validation.ValidRequired(nil, "Name") })
		assert.Error(t, err)
	})

	t.Run("typed nil pointer", func(t *testing.T) {
		var err error
		assert.NotPanics(t, func() { err = validation.ValidRequired((*req)(nil), "Name") })
		assert.Error(t, err)
	})

	t.Run("non struct", func(t *testing.T) {
		var err error
		assert.NotPanics(t, func() { err = validation.ValidRequired(1, "Name") })
		assert.Error(t, err)

		assert.NotPanics(t, func() { _ = validation.ValidRequired([]string{"a"}, "Name") })
		assert.NotPanics(t, func() { _ = validation.ValidRequired(note, "Name") })
	})

	t.Run("pointer to non struct", func(t *testing.T) {
		var err error
		assert.NotPanics(t, func() { err = validation.ValidRequired(&note, "Name") })
		assert.Error(t, err)
	})

	t.Run("valid", func(t *testing.T) {
		assert.NoError(t, validation.ValidRequired(ptr, "Name"))
	})
}

func TestValidateAllFields(t *testing.T) {
	type req struct {
		Name string
		Age  int
	}

	rules := validation.Rules{
		"Name": func(value any) error {
			if value.(string) == "" {
				return errors.New("Name is required")
			}
			return nil
		},
	}

	assert.EqualError(t, validation.ValidateAllFields(&req{}, rules), "Name is required")
	assert.NoError(t, validation.ValidateAllFields(&req{Name: "n"}, rules))
}

func TestGetExportedFieldNames(t *testing.T) {
	type req struct {
		Name string
		age  int
	}

	assert.Equal(t, []string{"Name"}, validation.GetExportedFieldNames(&req{}))
	assert.Equal(t, []string{"Name"}, validation.GetExportedFieldNames(req{}))
	assert.Equal(t, []string{}, validation.GetExportedFieldNames(1))
}

// TestNilInput 覆盖导出函数收到 nil 入参时不得 panic.
func TestNilInput(t *testing.T) {
	assert.NotPanics(t, func() {
		assert.Equal(t, []string{}, validation.GetExportedFieldNames(nil))
	})
	assert.NotPanics(t, func() {
		assert.Error(t, validation.ValidateSelectedFields(nil, nil, "Name"))
	})
	assert.NotPanics(t, func() {
		assert.Error(t, validation.ValidateAllFields(nil, nil))
	})
	assert.NotPanics(t, func() {
		assert.Error(t, validation.ValidateSelectedFields(1, nil, "Name"))
	})
}
