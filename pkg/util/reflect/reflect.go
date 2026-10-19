package reflect

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrNilReceiver 表示接收者为 nil 或 nil 指针.
	ErrNilReceiver = errors.New("reflectutil: nil receiver")
	// ErrNotStruct 表示接收者不是结构体（也不是指向结构体的指针）.
	ErrNotStruct = errors.New("reflectutil: not a struct")
	// ErrUnknownField 表示指定的字段不存在.
	ErrUnknownField = errors.New("reflectutil: unknown field")
	// ErrUndefGormField 表示字段没有定义 gorm column.
	ErrUndefGormField = errors.New("reflectutil: undefined gorm field")
	// ErrUnexportedField 表示字段未导出，无法读取.
	ErrUnexportedField = errors.New("reflectutil: unexported field")
	// ErrInvalidTarget 表示拷贝目标必须是「非 nil 的、指向结构体的指针」.
	ErrInvalidTarget = errors.New("reflectutil: target must be a non-nil pointer to struct")
)

// ToGormDBMap 将结构体按 gorm column 名抽取为 map，值均为 Go 原生值.
//
// 相比旧实现的行为修复：
//   - 写入 map 的是字段的原生值（v.Interface()），而不是 reflect.Value
//     （旧实现得到的是 map[id:<int Value>] 这类 reflect.Value）；
//   - 传入 nil / nil 指针 / 切片 / 非结构体时返回 error，不再 panic；
//   - 未导出字段无法读取时返回 error。
func ToGormDBMap(obj any, fields []string) (map[string]any, error) {
	v := reflect.ValueOf(obj)
	if !v.IsValid() {
		return nil, ErrNilReceiver
	}
	// 只解引用指针；切片等其它类型一律视为不支持（旧实现在切片上会 panic）.
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, ErrNilReceiver
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: got %s", ErrNotStruct, v.Kind())
	}

	reflectType := v.Type()
	ret := make(map[string]any, 0)
	for _, f := range fields {
		fs, exist := reflectType.FieldByName(f)
		if !exist {
			return nil, fmt.Errorf("%w: %s", ErrUnknownField, f)
		}

		tagMap := parseTagSetting(fs.Tag)
		gormfiled, exist := tagMap["COLUMN"]
		if !exist {
			return nil, fmt.Errorf("%w: %s", ErrUndefGormField, f)
		}

		fieldValue := v.FieldByName(f)
		if !fieldValue.CanInterface() {
			return nil, fmt.Errorf("%w: %s", ErrUnexportedField, f)
		}
		// 关键修复：存原生值，而不是 reflect.Value.
		ret[gormfiled] = fieldValue.Interface()
	}
	return ret, nil
}

func parseTagSetting(tags reflect.StructTag) map[string]string {
	setting := map[string]string{}
	for _, str := range []string{tags.Get("sql"), tags.Get("gorm")} {
		if str == "" {
			continue
		}
		tags := strings.Split(str, ";")
		for _, value := range tags {
			v := strings.Split(value, ":")
			k := strings.TrimSpace(strings.ToUpper(v[0]))
			if len(v) >= 2 {
				setting[k] = strings.Join(v[1:], ":")
			} else {
				setting[k] = k
			}
		}
	}
	return setting
}

// GetObjFieldsMap 返回「字段名 -> 值」的映射.
//
// 为保持向后兼容，签名不返回 error；对非法输入做安全降级（返回空 map），绝不 panic。
// 语义：
//   - 接收者为 nil / nil 指针 / 非结构体：返回空 map；
//   - 未导出字段：无法读取，跳过（旧实现调用 Interface() 会 panic）；
//   - nil 指针字段：保留为 typed nil（旧实现会 panic）；
//   - 非 nil 的结构体字段 / 指向结构体的指针字段：递归展开为嵌套 map；
//   - 指向非结构体的指针字段：保留指针本身。
//
// 需要区分错误原因、拿到具体 error 时，请使用 GetObjFieldsMapE。
func GetObjFieldsMap(obj any, fields []string) map[string]any {
	m, err := GetObjFieldsMapE(obj, fields)
	if err != nil {
		return map[string]any{}
	}
	return m
}

// GetObjFieldsMapE 是 GetObjFieldsMap 的带 error 版本，语义完全一致，
// 区别在于会返回可判别的错误（可用 errors.Is 判断 ErrNilReceiver / ErrNotStruct）。
func GetObjFieldsMapE(obj any, fields []string) (map[string]any, error) {
	if obj == nil {
		return nil, ErrNilReceiver
	}

	v := reflect.ValueOf(obj)
	if !v.IsValid() {
		return nil, ErrNilReceiver
	}
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, ErrNilReceiver
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: got %s", ErrNotStruct, v.Kind())
	}

	return objFieldsMapFromValue(v, fields), nil
}

// objFieldsMapFromValue 从结构体的 reflect.Value 抽取字段映射，不 panic.
func objFieldsMapFromValue(v reflect.Value, fields []string) map[string]any {
	modelRefType := v.Type()
	fieldsCount := v.NumField()
	ret := make(map[string]any, fieldsCount)

	var fieldData any
	for i := range fieldsCount {
		field := v.Field(i)
		fieldType := modelRefType.Field(i)
		if len(fields) != 0 && !findString(fields, fieldType.Name) {
			continue
		}
		// 未导出字段无法 Interface()，跳过以免 panic.
		if !field.CanInterface() {
			continue
		}

		switch field.Kind() {
		case reflect.Struct:
			fieldData = objFieldsMapFromValue(field, nil)
		case reflect.Ptr:
			switch {
			case field.IsNil():
				// 保留 typed nil，避免对无效 Value 继续操作.
				fieldData = field.Interface()
			case field.Type().Elem().Kind() == reflect.Struct:
				fieldData = objFieldsMapFromValue(field.Elem(), nil)
			default:
				fieldData = field.Interface()
			}
		default:
			fieldData = field.Interface()
		}

		ret[fieldType.Name] = fieldData
	}

	return ret
}

// CopyObj 将 from 中（按 fields 过滤后的）字段拷贝到 to.
//
// to 必须是「非 nil 的、指向结构体的指针」，否则返回 ErrInvalidTarget（旧实现会 panic）。
// 不可设置（未导出）或类型不可赋值的字段会被安全跳过。
func CopyObj(from any, to any, fields []string) (changed bool, err error) {
	fromMap, err := GetObjFieldsMapE(from, fields)
	if err != nil {
		return false, err
	}
	toMap, err := GetObjFieldsMapE(to, fields)
	if err != nil {
		return false, err
	}
	if reflect.DeepEqual(fromMap, toMap) {
		return false, nil
	}

	tv := reflect.ValueOf(to)
	if tv.Kind() != reflect.Ptr || tv.IsNil() {
		return false, ErrInvalidTarget
	}
	tv = tv.Elem()
	if tv.Kind() != reflect.Struct {
		return false, ErrInvalidTarget
	}

	for k, v := range fromMap {
		val := tv.FieldByName(k)
		if !val.IsValid() || !val.CanSet() {
			continue
		}
		rv := reflect.ValueOf(v)
		if !rv.IsValid() || !rv.Type().AssignableTo(val.Type()) {
			continue
		}
		val.Set(rv)
	}
	return true, nil
}

// CopyObjViaYaml marshal "from" to yaml data, then unMarshal data to "to".
func CopyObjViaYaml(to any, from any) error {
	if from == nil || to == nil {
		return nil
	}

	data, err := yaml.Marshal(from)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, to)
}

// StructName used to get the struct name from the obj.
func StructName(obj any) string {
	t := reflect.TypeOf(obj)
	if t.Kind() == reflect.Ptr {
		return t.Elem().Name()
	}

	return t.Name()
}

// findString return true if target in slice, return false if not.
func findString(slice []string, target string) bool {
	return slices.Contains(slice, target)
}
