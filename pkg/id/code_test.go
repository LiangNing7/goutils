package id

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCode(t *testing.T) {
	type args struct {
		id      uint64
		options []func(*CodeOptions)
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{name: "default", args: args{id: 1}, want: "VHB4JX86"},
		{
			name: "with-options",
			args: args{
				id: 1,
				options: []func(*CodeOptions){
					WithCodeChars([]rune{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}),
					WithCodeN1(9),
					WithCodeN2(3),
					WithCodeL(5),
					WithCodeSalt(56789),
				},
			},
			want: "80773",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewCode(tt.args.id, tt.args.options...))
		})
	}
}

func BenchmarkNewCode(b *testing.B) {
	for b.Loop() {
		NewCode(1)
	}
}

// wideCharset 返回包含 n 个互不相同 rune 的字符集.
func wideCharset(n int) []rune {
	chars := make([]rune, n)
	for i := range chars {
		chars[i] = rune(0x4E00 + i) // CJK 统一表意文字区，足够宽且互不重复
	}
	return chars
}

// assertCodeValid 断言 code 由 chars 中的字符组成且长度为 l.
func assertCodeValid(t *testing.T, code string, chars []rune, l int) {
	t.Helper()
	runes := []rune(code)
	assert.Equal(t, l, len(runes), "code %q rune length", code)
	set := make(map[rune]struct{}, len(chars))
	for _, c := range chars {
		set[c] = struct{}{}
	}
	for _, r := range runes {
		_, ok := set[r]
		assert.True(t, ok, "code %q contains rune %q which is not in charset", code, r)
	}
}

// TestNewCode_WideCharset 覆盖 C11: 字符集长度 >= 256 时 byte(charLen) 溢出为 0 导致整数除零 panic.
func TestNewCode_WideCharset(t *testing.T) {
	for _, n := range []int{256, 257, 300} {
		chars := wideCharset(n)
		t.Run(fmt.Sprintf("charset-%d-l6", n), func(t *testing.T) {
			var code string
			assert.NotPanics(t, func() {
				code = NewCode(1, WithCodeChars(chars), WithCodeL(6), WithCodeSalt(56789))
			})
			assertCodeValid(t, code, chars, 6)

			// 可重复性
			assert.Equal(t, code, NewCode(1, WithCodeChars(chars), WithCodeL(6), WithCodeSalt(56789)))
			// 不同 id 一般得到不同 code
			assert.NotEqual(t, code, NewCode(2, WithCodeChars(chars), WithCodeL(6), WithCodeSalt(56789)))
		})

		t.Run(fmt.Sprintf("charset-%d-default-l", n), func(t *testing.T) {
			var code string
			assert.NotPanics(t, func() {
				code = NewCode(1, WithCodeChars(chars))
			})
			assertCodeValid(t, code, chars, 8)
		})
	}
}

// TestNewCode_LengthEdgeCases 覆盖 l 的边界: 0 / 1 / 上限 / 超上限，均不得 panic.
func TestNewCode_LengthEdgeCases(t *testing.T) {
	t.Run("l=0 保持默认值", func(t *testing.T) {
		var code string
		assert.NotPanics(t, func() { code = NewCode(1, WithCodeL(0)) })
		// WithCodeL 忽略 <=0 的值，沿用默认长度 8
		assert.Equal(t, 8, len([]rune(code)))
		assert.Equal(t, NewCode(1), code)
	})

	t.Run("l=1", func(t *testing.T) {
		var code string
		assert.NotPanics(t, func() { code = NewCode(1, WithCodeL(1)) })
		assert.Equal(t, 1, len([]rune(code)))
	})

	t.Run("l=MaxCodeLength 合法", func(t *testing.T) {
		chars := wideCharset(300)
		var code string
		assert.NotPanics(t, func() {
			code = NewCode(1, WithCodeChars(chars), WithCodeL(MaxCodeLength))
		})
		assertCodeValid(t, code, chars, MaxCodeLength)

		_, err := NewCodeE(1, WithCodeChars(chars), WithCodeL(MaxCodeLength))
		assert.NoError(t, err)
	})

	t.Run("l>MaxCodeLength 非法", func(t *testing.T) {
		chars := wideCharset(300)
		_, err := NewCodeE(1, WithCodeChars(chars), WithCodeL(MaxCodeLength+1))
		assert.Error(t, err)

		// NewCode 退回默认配置：不 panic 且返回默认长度的合法 code
		var code string
		assert.NotPanics(t, func() { code = NewCode(1, WithCodeChars(chars), WithCodeL(1<<20)) })
		assert.Equal(t, NewCode(1), code)
		assertCodeValid(t, code, getCodeOptionsOrSetDefault(nil).chars, 8)
	})

	t.Run("l 极大值不 panic", func(t *testing.T) {
		assert.NotPanics(t, func() { NewCode(1, WithCodeL(math.MaxInt)) })
	})
}

// TestNewCode_CharsetEdgeCases 覆盖字符集过小的场景.
func TestNewCode_CharsetEdgeCases(t *testing.T) {
	t.Run("charset=1 非法", func(t *testing.T) {
		_, err := NewCodeE(1, WithCodeChars([]rune{'x'}))
		assert.Error(t, err)

		var code string
		assert.NotPanics(t, func() { code = NewCode(1, WithCodeChars([]rune{'x'})) })
		// 退回默认配置
		assert.Equal(t, NewCode(1), code)
	})

	t.Run("空字符集被忽略", func(t *testing.T) {
		var code string
		assert.NotPanics(t, func() { code = NewCode(1, WithCodeChars(nil)) })
		assert.Equal(t, NewCode(1), code)
	})
}

// TestNewCodeE 校验非法配置返回错误、合法配置与 NewCode 一致.
func TestNewCodeE(t *testing.T) {
	code, err := NewCodeE(1)
	assert.NoError(t, err)
	assert.Equal(t, "VHB4JX86", code)
	assert.Equal(t, NewCode(1), code)

	// 直接构造非法配置（字符集为空）
	_, err = NewCodeE(1, func(o *CodeOptions) { o.chars = nil })
	assert.Error(t, err)

	// 直接构造非法配置（l 为 0）
	_, err = NewCodeE(1, func(o *CodeOptions) { o.l = 0 })
	assert.Error(t, err)

	// 直接构造非法配置（l 为负数）
	_, err = NewCodeE(1, func(o *CodeOptions) { o.l = -1 })
	assert.Error(t, err)
}

// TestNewCode_NegativeN2 覆盖 n2 为负数时索引不得越界.
func TestNewCode_NegativeN2(t *testing.T) {
	var code string
	assert.NotPanics(t, func() {
		code = NewCode(1, WithCodeN2(-3), WithCodeL(5))
	})
	assert.Equal(t, 5, len([]rune(code)))
}

func BenchmarkNewCodeTimeConsuming(b *testing.B) {
	b.StopTimer() // 调用该函数停止压力测试的时间计数

	id := NewCode(1)
	assert.Equal(b, "VHB4JX86", id)

	b.StartTimer() // 重新开始时间

	for b.Loop() {
		NewCode(1)
	}
}
