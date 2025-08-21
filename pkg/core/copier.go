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
func CopyWithConverters(to any, from any) error {
	return copier.CopyWithOption(
		to,
		from,
		copier.Option{
			IgnoreEmpty: true,             // 忽略源中空值字段.
			DeepCopy:    true,             // 启用深度拷贝.
			Converters:  TypeConverters(), // 应用自定义类型转换器
		})
}

// CopyWithCustomConverters 使用自定义转换器.
// to: 目标结构体指针; from: 源结构体指针；customConverters 自定义转换器.
func CopyWithCustomConverters(to any, from any, customConverters []copier.TypeConverter) error {
	defaultConverters := TypeConverters()
	customConverters = append(customConverters, defaultConverters...)
	return copier.CopyWithOption(
		to,
		from,
		copier.Option{
			IgnoreEmpty: true,             // 忽略源中空值字段.
			DeepCopy:    true,             // 启用深度拷贝.
			Converters:  customConverters, // 应用自定义类型转换器
		})
}

// Copy 普通拷贝，不应用自定义转换，只做浅拷贝并赋值同名字段.
func Copy(to any, from any) error {
	return copier.Copy(to, from)
}

// CopyValueWithCustomConverters 将 from 的值根据 converters 中定义的转换规则
// 转换后赋值给 to 指向的变量。
//
// 参数说明：
//   - to:        目标值的指针 (例如 *string)，用于接收转换后的结果。
//     必须是指针，否则无法通过反射赋值。
//   - from:      源值 (例如 string 或 UserRole)，类型必须和 converters 中的 SrcType 匹配。
//   - converters: 自定义转换器。
//
// 返回值：
//   - error: 转换成功时为 nil；若未找到合适的转换器或转换失败，则返回错误。
func CopyValueWithCustomConverters(to any, from any, converters []copier.TypeConverter) error {
	dstType := reflect.TypeOf(to).Elem() // 指针指向的类型
	srcType := reflect.TypeOf(from)

	for _, c := range converters {
		if c.SrcType == srcType && c.DstType == dstType {
			v, err := c.Fn(from)
			if err != nil {
				return err
			}
			reflect.ValueOf(to).Elem().Set(reflect.ValueOf(v))
			return nil
		}
	}
	return fmt.Errorf("no converter found for %v -> %v", srcType, dstType)
}
