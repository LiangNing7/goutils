package core

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jinzhu/copier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/datatypes"
)

type zeroSrc struct {
	B bool
	N int
	P string
}

type zeroDst struct {
	B bool
	N int
	P string
}

// --- CopyValueWithCustomConverters：C9 类型比较恒为 false ---

func TestCopyValueWithCustomConverters_TimeToTimestamp(t *testing.T) {
	want := time.Date(2026, 10, 19, 12, 30, 45, 123456789, time.UTC)

	// 转换器的 DstType 是 *timestamppb.Timestamp，因此 to 必须指向一个 *timestamppb.Timestamp 变量
	var dst *timestamppb.Timestamp
	require.NoError(t, CopyValueWithCustomConverters(&dst, want, TypeConverters()))

	require.NotNil(t, dst)
	require.True(t, dst.IsValid(), "转换结果应当是合法的 Timestamp")
	assert.True(t, dst.AsTime().Equal(want), "want %v, got %v", want, dst.AsTime())
}

func TestCopyValueWithCustomConverters_TimestampToTime(t *testing.T) {
	want := time.Date(2026, 10, 19, 12, 30, 45, 123456789, time.UTC)

	var dst time.Time
	require.NoError(t, CopyValueWithCustomConverters(&dst, timestamppb.New(want), TypeConverters()))

	assert.True(t, dst.Equal(want), "want %v, got %v", want, dst)
}

func TestCopyValueWithCustomConverters_JSONToStruct(t *testing.T) {
	var dst *structpb.Struct
	require.NoError(t, CopyValueWithCustomConverters(&dst, datatypes.JSON(`{"a":1,"b":"x"}`), TypeConverters()))

	require.NotNil(t, dst)
	m := dst.AsMap()
	assert.Equal(t, float64(1), m["a"])
	assert.Equal(t, "x", m["b"])
}

func TestCopyValueWithCustomConverters_StructToJSON(t *testing.T) {
	s, err := structpb.NewStruct(map[string]any{"a": 1.0, "b": "x"})
	require.NoError(t, err)

	var dst datatypes.JSON
	require.NoError(t, CopyValueWithCustomConverters(&dst, s, TypeConverters()))

	var got map[string]any
	require.NoError(t, json.Unmarshal(dst, &got))
	assert.Equal(t, float64(1), got["a"])
	assert.Equal(t, "x", got["b"])
}

// --- C2'：非指针入参 panic ---

func TestCopyValueWithCustomConverters_NonPointerTarget(t *testing.T) {
	var s string

	assert.NotPanics(t, func() {
		err := CopyValueWithCustomConverters(s, "x", nil)
		require.Error(t, err)
	})
}

func TestCopyValueWithCustomConverters_NilTarget(t *testing.T) {
	assert.NotPanics(t, func() {
		require.Error(t, CopyValueWithCustomConverters(nil, "x", nil))
	})
}

func TestCopyValueWithCustomConverters_TypedNilPointerTarget(t *testing.T) {
	var p *string

	assert.NotPanics(t, func() {
		// 转换器匹配成功，但 to 是一个 nil 指针，不能对其 Elem() 赋值
		converters := []copier.TypeConverter{{
			SrcType: "src",
			DstType: "",
			Fn:      func(src any) (any, error) { return src, nil },
		}}
		require.Error(t, CopyValueWithCustomConverters(p, "src", converters))
	})
}

func TestCopyValueWithCustomConverters_ConverterReturnsMismatchedType(t *testing.T) {
	dst := new(string)

	assert.NotPanics(t, func() {
		converters := []copier.TypeConverter{{
			SrcType: 0,  // int
			DstType: "", // string
			Fn:      func(any) (any, error) { return 42, nil },
		}}
		err := CopyValueWithCustomConverters(dst, 5, converters)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not assignable")
	})
	assert.Equal(t, "", *dst)
}

func TestCopyValueWithCustomConverters_ConverterReturnsNil(t *testing.T) {
	dst := new(string)

	assert.NotPanics(t, func() {
		converters := []copier.TypeConverter{{
			SrcType: 0,
			DstType: "",
			Fn:      func(any) (any, error) { return nil, nil },
		}}
		assert.Error(t, CopyValueWithCustomConverters(dst, 5, converters))
	})
}

func TestCopyValueWithCustomConverters_ConverterWithoutFn(t *testing.T) {
	dst := new(string)

	assert.NotPanics(t, func() {
		// 类型对的上，但没有提供 Fn
		assert.Error(t, CopyValueWithCustomConverters(dst, 5, []copier.TypeConverter{{SrcType: 0, DstType: ""}}))
	})
}

func TestCopyValueWithCustomConverters_NilSourceWithTypedNilConverters(t *testing.T) {
	var dst any

	assert.NotPanics(t, func() {
		// from 为 nil 时 srcType 为 nil；未声明 SrcType/DstType 的转换器不应被误匹配
		assert.Error(t, CopyValueWithCustomConverters(&dst, nil, []copier.TypeConverter{{}}))
	})
}

func TestCopyValueWithCustomConverters_NoConverterFound(t *testing.T) {
	dst := new(string)

	assert.NotPanics(t, func() {
		assert.Error(t, CopyValueWithCustomConverters(dst, 5, TypeConverters()))
	})
	assert.Equal(t, "", *dst)
}

// --- 别名污染 ---

func TestCopyWithCustomConverters_DoesNotMutateCallerSlice(t *testing.T) {
	custom := make([]copier.TypeConverter, 1, 8)
	custom[0] = copier.TypeConverter{
		SrcType: "custom-src",
		DstType: "custom-dst",
		Fn:      func(src any) (any, error) { return src, nil },
	}
	// 调用方 slice 的底层数组视图：cap 为 8，append 默认转换器时若直接 append 会写进来
	full := custom[:cap(custom)]

	var out zeroDst
	require.NoError(t, CopyWithCustomConverters(&out, &zeroSrc{P: "x"}, custom))
	assert.Equal(t, "x", out.P)

	assert.Len(t, custom, 1, "不应修改调用方 slice 的长度")
	for i := 1; i < len(full); i++ {
		assert.Nilf(t, full[i].SrcType, "调用方底层数组 index=%d 的 SrcType 被污染", i)
		assert.Nilf(t, full[i].DstType, "调用方底层数组 index=%d 的 DstType 被污染", i)
	}
}

// --- IgnoreEmpty：旧函数保留既有语义，新函数支持显式拷贝零值 ---

func TestCopyWithConverters_IgnoresZeroValues(t *testing.T) {
	dst := zeroDst{B: true, N: 5, P: "keep"}
	require.NoError(t, CopyWithConverters(&dst, &zeroSrc{}))

	// 既有语义（有意为之）：源中的零值不覆盖目标
	assert.Equal(t, zeroDst{B: true, N: 5, P: "keep"}, dst)
}

func TestCopyWithConverters_CopiesNonZeroValues(t *testing.T) {
	dst := zeroDst{}
	require.NoError(t, CopyWithConverters(&dst, &zeroSrc{B: true, N: 5, P: "v"}))

	assert.Equal(t, zeroDst{B: true, N: 5, P: "v"}, dst)
}

func TestCopyWithConvertersStrict_CopiesZeroValues(t *testing.T) {
	dst := zeroDst{B: true, N: 5, P: "keep"}
	require.NoError(t, CopyWithConvertersStrict(&dst, &zeroSrc{B: false, N: 0, P: ""}))

	// 零值也必须拷贝过去：把 bool 置 false、计数置 0、字符串置空
	assert.Equal(t, zeroDst{B: false, N: 0, P: ""}, dst)
}

func TestCopyWithConvertersStrict_CopiesNonZeroValues(t *testing.T) {
	dst := zeroDst{}
	require.NoError(t, CopyWithConvertersStrict(&dst, &zeroSrc{B: true, N: 5, P: "v"}))

	assert.Equal(t, zeroDst{B: true, N: 5, P: "v"}, dst)
}

type convSrc struct {
	TS time.Time
}

type convDst struct {
	TS *timestamppb.Timestamp
}

func TestCopyWithConvertersStrict_StillAppliesConverters(t *testing.T) {
	want := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)

	var dst convDst
	require.NoError(t, CopyWithConvertersStrict(&dst, &convSrc{TS: want}))

	require.NotNil(t, dst.TS)
	assert.True(t, dst.TS.AsTime().Equal(want), "want %v, got %v", want, dst.TS.AsTime())
}

func TestCopyWithOptions(t *testing.T) {
	// 默认与 CopyWithConverters 一致：忽略零值
	dst := zeroDst{B: true, N: 5, P: "keep"}
	require.NoError(t, CopyWithOptions(&dst, &zeroSrc{}))
	assert.Equal(t, zeroDst{B: true, N: 5, P: "keep"}, dst)

	// WithIgnoreEmpty(true) 显式忽略零值
	dst = zeroDst{B: true, N: 5, P: "keep"}
	require.NoError(t, CopyWithOptions(&dst, &zeroSrc{}, WithIgnoreEmpty(true)))
	assert.Equal(t, zeroDst{B: true, N: 5, P: "keep"}, dst)

	// WithIgnoreEmpty(false) 等价于 Strict：零值也拷贝
	dst = zeroDst{B: true, N: 5, P: "keep"}
	require.NoError(t, CopyWithOptions(&dst, &zeroSrc{}, WithIgnoreEmpty(false)))
	assert.Equal(t, zeroDst{}, dst)
}

type convOptSrc struct {
	V int
}

type convOptDst struct {
	V string
}

func TestCopyWithOptions_WithConverters(t *testing.T) {
	// 自定义转换器：int -> string
	converters := []copier.TypeConverter{{
		SrcType: 0,
		DstType: "",
		Fn:      func(src any) (any, error) { return fmt.Sprintf("v=%v", src), nil },
	}}

	var dst convOptDst
	require.NoError(t, CopyWithOptions(&dst, &convOptSrc{V: 7}, WithIgnoreEmpty(false), WithConverters(converters...)))
	assert.Equal(t, "v=7", dst.V)
}

func TestCopyWithOptions_WithConvertersDoesNotMutateCallerSlice(t *testing.T) {
	custom := make([]copier.TypeConverter, 1, 8)
	custom[0] = copier.TypeConverter{
		SrcType: "custom-src",
		DstType: "custom-dst",
		Fn:      func(src any) (any, error) { return src, nil },
	}
	full := custom[:cap(custom)]

	var dst zeroDst
	require.NoError(t, CopyWithOptions(&dst, &zeroSrc{P: "x"}, WithConverters(custom...)))

	for i := 1; i < len(full); i++ {
		assert.Nilf(t, full[i].SrcType, "调用方底层数组 index=%d 的 SrcType 被污染", i)
		assert.Nilf(t, full[i].DstType, "调用方底层数组 index=%d 的 DstType 被污染", i)
	}
}

// --- Copy：既有浅拷贝行为不变 ---

func TestCopy(t *testing.T) {
	dst := zeroDst{B: true, N: 5, P: "keep"}
	require.NoError(t, Copy(&dst, &zeroSrc{B: true, N: 1, P: "new"}))

	// Copy 是浅拷贝同名字段，不做零值忽略
	assert.Equal(t, zeroDst{B: true, N: 1, P: "new"}, dst)
}
