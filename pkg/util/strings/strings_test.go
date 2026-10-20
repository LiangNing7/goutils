package strings

import (
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	testCase := [][]string{
		{"foo", "bar", "hello"},
		{"foo", "bar", "world"},
	}
	result := Diff(testCase[0], testCase[1])
	if len(result) != 1 || result[0] != "hello" {
		t.Fatalf("Diff failed")
	}
}

// TestReverse 覆盖合法 UTF-8、非法 UTF-8 字节序列、空串与纯 ASCII 四种输入，
// 断言 Reverse 不会 panic 且结果符合文档语义。
func TestReverse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// 合法多字节 UTF-8：按 rune 反转，多字节字符不被拆散.
		{"multibyte-cjk", "中文abc", "cba文中"},
		{"multibyte-mixed", "Hello, 世界", "界世 ,olleH"},
		// 非法 UTF-8 字节序列：DecodeRune 语义下 \xff 被规整为 U+FFFD，结果确定.
		{"invalid-byte", "\xffabc", "cba�"},
		{"invalid-only", "\xff\xfe", "��"},
		// 边界：空串与纯 ASCII.
		{"empty", "", ""},
		{"ascii", "abc", "cba"},
		{"single-rune", "中", "中"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Reverse(%q) panicked: %v", c.in, r)
				}
			}()
			if got := Reverse(c.in); got != c.want {
				t.Fatalf("Reverse(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestUniqueOrderStable 断言 Unique 的输出顺序是确定的（按首次出现顺序），
// 多次调用结果一致，不依赖 map 迭代顺序.
func TestUniqueOrderStable(t *testing.T) {
	in := []string{"b", "a", "b", "c", "a"}
	want := []string{"b", "a", "c"}

	first := Unique(in)
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("Unique = %v, want %v", first, want)
	}
	for i := 0; i < 100; i++ {
		if got := Unique(in); !reflect.DeepEqual(got, first) {
			t.Fatalf("Unique is not stable: run %d = %v, want %v", i, got, first)
		}
	}
}

// TestFrequencySortStable 断言 FrequencySort 在频次相同时使用确定性次序
// （按首次出现顺序），多次调用结果一致.
func TestFrequencySortStable(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		// 全部频次为 1：应按首次出现顺序返回.
		{
			"all-singletons",
			[]string{"g", "h", "a", "b", "c", "d", "e", "f"},
			[]string{"g", "h", "a", "b", "c", "d", "e", "f"},
		},
		// 频次不同：按频次降序.
		{"mixed-frequency", []string{"a", "a", "b", "c", "c", "c"}, []string{"c", "a", "b"}},
		// 频次相同：按首次出现顺序（b 首次出现在下标 0，a 在下标 1）.
		{"tie-first-seen", []string{"b", "a", "a", "b"}, []string{"b", "a"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first := FrequencySort(c.in)
			if !reflect.DeepEqual(first, c.want) {
				t.Fatalf("FrequencySort(%v) = %v, want %v", c.in, first, c.want)
			}
			for i := 0; i < 100; i++ {
				if got := FrequencySort(c.in); !reflect.DeepEqual(got, first) {
					t.Fatalf("FrequencySort is not stable: run %d = %v, want %v", i, got, first)
				}
			}
		})
	}
}
