package core

import (
	"errors"
	"time"

	"github.com/jinzhu/copier"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TypeConverters 定义时间类型转换器，用于 copier 的深度拷贝.
// 主要用于在 Go 原生的 time.Time 与 Protobuf 内置的 Timestamp 之间进行相互转换.
func TypeConverters() []copier.TypeConverter {
	return []copier.TypeConverter{
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
	}
}

// CopyWithConverters 使用自定义转换器，支持深拷贝并忽略零值字段.
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

// Copy 普通拷贝，不应用自定义转换，只做浅拷贝并赋值同名字段.
func Copy(to any, from any) error {
	return copier.Copy(to, from)
}
