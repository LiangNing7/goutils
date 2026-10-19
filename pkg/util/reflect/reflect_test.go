package reflect

import (
	"reflect"
	"testing"
)

func TestGetObjFieldsMap(t *testing.T) {
	type Obj struct {
		A int
		B int
		C int
	}

	org := &Obj{
		A: 1,
		B: 2,
		C: 3,
	}

	m := GetObjFieldsMap(org, []string{})
	if !reflect.DeepEqual(m, map[string]any{
		"A": 1,
		"B": 2,
		"C": 3,
	}) {
		t.Fatalf("not equal")
	}

	m = GetObjFieldsMap(org, []string{"A"})
	if !reflect.DeepEqual(m, map[string]any{
		"A": 1,
	}) {
		t.Fatalf("not equal")
	}
}

func TestCopyObj(t *testing.T) {
	type Obj struct {
		A int
		B int
		C int
	}

	org := &Obj{
		A: 1,
		B: 2,
		C: 3,
	}

	des := &Obj{
		A: 4,
		B: 5,
		C: 6,
	}

	changed, err := CopyObj(org, des, []string{"A"})
	if err != nil {
		t.Fatalf("%s", err.Error())
	}

	if !changed {
		t.Fatalf("expect changed")
	}

	if des.A != org.A {
		t.Fatalf("A not copy")
	}

	if des.B != 5 || des.C != 6 {
		t.Fatalf("B and C changed")
	}

	des.A = org.A
	changed, err = CopyObj(org, des, []string{"A"})
	if err != nil {
		t.Fatalf("%s", err.Error())
	}

	if changed {
		t.Fatalf("expect not changed")
	}
}

type reflectInner struct {
	X int
}

type reflectObj struct {
	A      int
	b      int // 未导出字段
	P      *reflectInner
	Q      *int
	hidden int `gorm:"column:hidden"` // 未导出但带 gorm 标签
}

// TestGetObjFieldsMapUnexportedAndNilPtr 断言未导出字段被安全跳过、nil 指针字段不会引发 panic.
func TestGetObjFieldsMapUnexportedAndNilPtr(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GetObjFieldsMap panicked: %v", r)
		}
	}()

	m := GetObjFieldsMap(&reflectObj{A: 7}, nil)

	if v, ok := m["A"].(int); !ok || v != 7 {
		t.Fatalf("m[\"A\"] = %#v, want 7", m["A"])
	}
	if _, ok := m["b"]; ok {
		t.Fatalf("unexported field b must be skipped, got %#v", m["b"])
	}
	if _, ok := m["hidden"]; ok {
		t.Fatalf("unexported field hidden must be skipped, got %#v", m["hidden"])
	}
	p, ok := m["P"]
	if !ok {
		t.Fatalf("nil pointer field P must be present")
	}
	if p != (*reflectInner)(nil) {
		t.Fatalf("nil pointer field P = %#v, want typed nil (*reflectInner)(nil)", p)
	}
}

// TestGetObjFieldsMapRecursesPtr 断言非 nil 的结构体指针字段会被递归展开.
func TestGetObjFieldsMapRecursesPtr(t *testing.T) {
	m := GetObjFieldsMap(&reflectObj{A: 1, P: &reflectInner{X: 42}}, nil)
	sub, ok := m["P"].(map[string]any)
	if !ok {
		t.Fatalf("m[\"P\"] = %#v, want nested map", m["P"])
	}
	if v, ok := sub["X"].(int); !ok || v != 42 {
		t.Fatalf("nested X = %#v, want 42", sub["X"])
	}
}

// TestGetObjFieldsMapSafeDegrade 断言 nil 接收者、nil 指针、非结构体输入都安全降级为空 map（不 panic）.
func TestGetObjFieldsMapSafeDegrade(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GetObjFieldsMap panicked: %v", r)
		}
	}()

	var nilPtr *reflectObj

	cases := []struct {
		name string
		obj  any
	}{
		{"nil-interface", nil},
		{"nil-pointer", nilPtr},
		{"non-struct", 123},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if m := GetObjFieldsMap(c.obj, nil); len(m) != 0 {
				t.Fatalf("GetObjFieldsMap(%#v) = %#v, want empty map", c.obj, m)
			}
		})
	}
}

// TestGetObjFieldsMapE 断言 *E 版本对错误输入返回可判别的 error.
func TestGetObjFieldsMapE(t *testing.T) {
	var nilPtr *reflectObj

	if _, err := GetObjFieldsMapE(nil, nil); err == nil {
		t.Fatal("GetObjFieldsMapE(nil) err = nil, want non-nil error")
	}
	if _, err := GetObjFieldsMapE(nilPtr, nil); err == nil {
		t.Fatal("GetObjFieldsMapE(nil pointer) err = nil, want non-nil error")
	}
	if _, err := GetObjFieldsMapE(123, nil); err == nil {
		t.Fatal("GetObjFieldsMapE(non-struct) err = nil, want non-nil error")
	}

	m, err := GetObjFieldsMapE(&reflectObj{A: 1}, []string{"A"})
	if err != nil {
		t.Fatalf("GetObjFieldsMapE(valid) unexpected error: %v", err)
	}
	if v, ok := m["A"].(int); !ok || v != 1 {
		t.Fatalf("m[\"A\"] = %#v, want 1", m["A"])
	}
}

// TestCopyObjNonPointerTarget 断言目标不是指针时返回错误而不是 panic.
func TestCopyObjNonPointerTarget(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CopyObj panicked: %v", r)
		}
	}()

	changed, err := CopyObj(&reflectObj{A: 1}, reflectObj{A: 2}, []string{"A"})
	if err == nil {
		t.Fatal("CopyObj with non-pointer target err = nil, want non-nil error")
	}
	if changed {
		t.Fatal("CopyObj with non-pointer target must not report changed")
	}
}

// TestCopyObjNilTarget 断言目标为 nil 指针时返回错误而不是 panic.
func TestCopyObjNilTarget(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CopyObj panicked: %v", r)
		}
	}()

	var dst *reflectObj
	changed, err := CopyObj(&reflectObj{A: 1}, dst, []string{"A"})
	if err == nil {
		t.Fatal("CopyObj with nil target err = nil, want non-nil error")
	}
	if changed {
		t.Fatal("CopyObj with nil target must not report changed")
	}
}

type gormTestModel struct {
	ID     int    `gorm:"column:id"`
	Name   string `gorm:"column:name"`
	NoTag  string
	hidden int `gorm:"column:hidden"`
}

// TestToGormDBMapValues 断言写入 map 的是 Go 原生值，而不是 reflect.Value.
func TestToGormDBMapValues(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ToGormDBMap panicked: %v", r)
		}
	}()

	m, err := ToGormDBMap(&gormTestModel{ID: 1, Name: "n"}, []string{"ID", "Name"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v, ok := m["id"].(int); !ok || v != 1 {
		t.Fatalf("m[\"id\"] = %#v, want native int 1", m["id"])
	}
	if v, ok := m["name"].(string); !ok || v != "n" {
		t.Fatalf("m[\"name\"] = %#v, want native string \"n\"", m["name"])
	}
}

// TestToGormDBMapPtrAndValue 断言指针与值两种接收者都能工作.
func TestToGormDBMapPtrAndValue(t *testing.T) {
	m, err := ToGormDBMap(gormTestModel{ID: 5}, []string{"ID"})
	if err != nil {
		t.Fatalf("unexpected error for value receiver: %v", err)
	}
	if v, ok := m["id"].(int); !ok || v != 5 {
		t.Fatalf("m[\"id\"] = %#v, want 5", m["id"])
	}
}

// TestToGormDBMapErrors 断言不支持/非法输入返回可判别的 error 而不是 panic.
func TestToGormDBMapErrors(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ToGormDBMap panicked: %v", r)
		}
	}()

	var nilPtr *gormTestModel

	cases := []struct {
		name   string
		obj    any
		fields []string
	}{
		{"nil", nil, []string{"ID"}},
		{"nil-pointer", nilPtr, []string{"ID"}},
		{"slice", []gormTestModel{{ID: 1}}, []string{"ID"}},
		{"non-struct", 123, []string{"ID"}},
		{"unknown-field", &gormTestModel{}, []string{"Nope"}},
		{"untagged-field", &gormTestModel{}, []string{"NoTag"}},
		{"unexported-field", &gormTestModel{}, []string{"hidden"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ToGormDBMap(c.obj, c.fields); err == nil {
				t.Fatalf("ToGormDBMap(%#v, %v) err = nil, want non-nil error", c.obj, c.fields)
			}
		})
	}
}
