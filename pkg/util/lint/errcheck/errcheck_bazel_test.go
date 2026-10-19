//go:build bazel
// +build bazel

package errcheck

import (
	"testing"

	"github.com/kisielk/errcheck/errcheck"
)

// TestInitDidNotPanicAndAppliedExcludes 断言 init() 未 panic，
// 且 errcheck_excludes.txt 中的排除项已被并入 DefaultExcludedSymbols。
//
// 旧实现在 init 中调用 Analyzer.Flags.Set("excludes", ...)：
// errcheck v1.8.0 不存在该 flag，Set 返回 error 后 panic(err)，
// 本测试（连同整个测试二进制）会在 init 阶段直接崩溃。
func TestInitDidNotPanicAndAppliedExcludes(t *testing.T) {
	found := false
	for _, s := range errcheck.DefaultExcludedSymbols {
		if s == "fmt.Println" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fmt.Println not found in DefaultExcludedSymbols: %v", errcheck.DefaultExcludedSymbols)
	}
}

func TestParseExcludes(t *testing.T) {
	got := parseExcludes("// comment\n\nfmt.Println\n  fmt.Printf  \n")
	want := []string{"fmt.Println", "fmt.Printf"}

	if len(got) != len(want) {
		t.Fatalf("parseExcludes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseExcludes[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
