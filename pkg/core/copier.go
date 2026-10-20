package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jinzhu/copier"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/datatypes"
)

// TypeConverters 定义自定义转换器，用于 copier 的深度拷贝.
func TypeConverters() []copier.TypeConverter {
	return []copier.TypeConverter{
		// 主要用于在 Go 原生的 time.Time 与 Protobuf 内置的 Timestamp 之间进行相互转换.
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

		// 主要用于在 GORM 中的 datatypes.JSON 与 Protobuf 的 *structpb.Struct 之间进行互相转换.
		{
			SrcType: datatypes.JSON{},
			DstType: &structpb.Struct{},
			Fn: func(src any) (any, error) {
				s, ok := src.(datatypes.JSON)
				if !ok {
					return nil, errors.New("source type not matching")
				}

				// 先转成 map[string]interface{}
				var m map[string]any
				if err := json.Unmarshal(s, &m); err != nil {
					return nil, err
				}

				// 再构造 structpb.Struct
				return structpb.NewStruct(m)
			},
		},
		{
			SrcType: &structpb.Struct{},
			DstType: datatypes.JSON{},
			Fn: func(src any) (any, error) {
				s, ok := src.(*structpb.Struct)
				if !ok {
					return nil, errors.New("source type not matching")
				}

				// 先转为 JSON 字符串
				b, err := json.Marshal(s.AsMap())
				if err != nil {
					return nil, err
				}

				// 再转成 datatypes.JSON
				return datatypes.JSON(b), nil
			},
		},
	}
}

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

// CopyOption 用于配置 CopyWithOptions 的拷贝行为.
type CopyOption func(*copyOptions)

// copyOptions 保存 CopyWithOptions 的解析后的配置.
type copyOptions struct {
	ignoreEmpty bool
	converters  []copier.TypeConverter
}

// WithIgnoreEmpty 设置是否忽略源中的零值字段（false / 0 / "" / nil）.
// 默认值为 true，与 CopyWithConverters 的既有语义保持一致；传入 false 时零值也会被拷贝.
func WithIgnoreEmpty(ignore bool) CopyOption {
	return func(o *copyOptions) {
		o.ignoreEmpty = ignore
	}
}

// WithConverters 追加自定义转换器.
// 注意：若自定义转换器与默认转换器（TypeConverters）注册的 (SrcType, DstType) 组合重复，
// 默认转换器会覆盖自定义转换器（与 CopyWithCustomConverters 的既有行为一致）.
func WithConverters(converters ...copier.TypeConverter) CopyOption {
	return func(o *copyOptions) {
		o.converters = append(o.converters, converters...)
	}
}

// CopyWithOptions 使用默认转换器做深拷贝，并允许通过 CopyOption 显式控制拷贝行为.
// 不传任何选项时，行为与 CopyWithConverters 完全一致（忽略零值）.
// to: 目标结构体指针; from: 源结构体指针.
func CopyWithOptions(to any, from any, opts ...CopyOption) error {
	o := &copyOptions{ignoreEmpty: true}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return copier.CopyWithOption(
		to,
		from,
		copier.Option{
			IgnoreEmpty: o.ignoreEmpty,                                   // 是否忽略源中空值字段.
			DeepCopy:    true,                                            // 启用深度拷贝.
			Converters:  mergeConverters(o.converters, TypeConverters()), // 应用类型转换器
		})
}

// mergeConverters 将自定义转换器与默认转换器合并为一个新的 slice。
// 这里必须新建 slice 后再 append：若直接在调用方传入的 slice 上 append，
// 当调用方 slice 的 cap 有余量时，会写入调用方的底层数组，造成「别名污染」.
func mergeConverters(custom, defaults []copier.TypeConverter) []copier.TypeConverter {
	all := make([]copier.TypeConverter, 0, len(custom)+len(defaults))
	all = append(all, custom...)
	all = append(all, defaults...)
	return all
}

// CopyWithCustomConverters 使用自定义转换器（在默认转换器之前注册）.
// to: 目标结构体指针; from: 源结构体指针；customConverters 自定义转换器.
func CopyWithCustomConverters(to any, from any, customConverters []copier.TypeConverter) error {
	return CopyWithOptions(to, from, WithConverters(customConverters...))
}

// Copy 普通拷贝，不应用自定义转换，只做浅拷贝并赋值同名字段.
func Copy(to any, from any) error {
	return copier.Copy(to, from)
}

// CopyValueWithCustomConverters 将 from 的值根据 converters 中定义的转换规则
// 转换后赋值给 to 指向的变量。
//
// 参数说明：
//   - to:        目标值的非 nil 指针 (例如 *string)。必须是非 nil 指针，否则返回错误（不会 panic）。
//   - from:      源值。其类型必须与某个转换器的 SrcType 匹配（nil 无法匹配任何转换器）。
//   - converters: 自定义转换器。转换器通过显式声明的 SrcType / DstType 匹配，
//     未声明 SrcType 或 DstType 的转换器（以及 Fn 为 nil 的转换器）不会被使用。
//
// 返回值：
//   - error: 转换成功时为 nil；若未找到合适的转换器、to 不是合法指针、
//     或转换器的返回值类型与 DstType 不匹配，则返回错误。
func CopyValueWithCustomConverters(to any, from any, converters []copier.TypeConverter) error {
	if to == nil {
		return errors.New("to must be a non-nil pointer, got nil")
	}

	dstValue := reflect.ValueOf(to)
	if dstValue.Kind() != reflect.Ptr {
		return fmt.Errorf("to must be a pointer, got %T", to)
	}
	// 类型可确定（指针），但指向 nil：对其 Elem() 赋值会 panic，必须提前拦截.
	if dstValue.IsNil() {
		return fmt.Errorf("to must be a non-nil pointer, got nil %T", to)
	}

	dstType := dstValue.Elem().Type() // 指针指向的类型
	srcType := reflect.TypeOf(from)

	for _, c := range converters {
		cSrcType, cDstType := reflect.TypeOf(c.SrcType), reflect.TypeOf(c.DstType)
		// 注意：c.SrcType / c.DstType 是 any，必须用 reflect.TypeOf 取动态类型后再比较，
		// 直接比较 any 与 reflect.Type 会恒为 false；
		// 而 reflect.TypeOf(nil) 返回 nil，未声明类型的转换器直接跳过，避免 nil == nil 的误匹配.
		if cSrcType == nil || cDstType == nil || cSrcType != srcType || cDstType != dstType {
			continue
		}
		if c.Fn == nil {
			return fmt.Errorf("converter %v -> %v has no conversion function", srcType, dstType)
		}

		v, err := c.Fn(from)
		if err != nil {
			return err
		}
		// Fn 返回 nil 时 reflect.ValueOf 得到的是无效 Value，不能用于 Set.
		if v == nil {
			return fmt.Errorf("converter %v -> %v returned a nil value", srcType, dstType)
		}

		rv := reflect.ValueOf(v)
		if !rv.Type().AssignableTo(dstType) {
			return fmt.Errorf("converter %v -> %v returned %v, which is not assignable to %v",
				srcType, dstType, rv.Type(), dstType)
		}

		dstValue.Elem().Set(rv)
		return nil
	}

	return fmt.Errorf("no converter found for %v -> %v", srcType, dstType)
}
