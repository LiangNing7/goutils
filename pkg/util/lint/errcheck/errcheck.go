// Package errcheck defines an Analyzer that is a slightly modified version
// of the errcheck Analyzer from upstream (github.com/kisielk/errcheck).
// Specifically, we pass in our own list of excludes.

//go:build bazel
// +build bazel

package errcheck

import (
	_ "embed"
	"strings"

	"github.com/kisielk/errcheck/errcheck"
)

var Analyzer = errcheck.Analyzer

//go:embed errcheck_excludes.txt
var excludesContent string

// parseExcludes 解析 newline 分隔的排除清单，忽略空行与以 "//" 开头的注释行.
// 格式与上游 errcheck.ReadExcludes 一致。
func parseExcludes(content string) []string {
	var excludes []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		excludes = append(excludes, line)
	}
	return excludes
}

func init() {
	// 修复：旧实现调用 Analyzer.Flags.Set("excludes", ...)，
	// 但 errcheck v1.8.0 中并不存在名为 "excludes" 的 flag（只有 "exclude"，且其值必须是文件路径），
	// FlagSet.Set 对未知 flag 返回 error，旧代码会 panic(err) 使程序在 init 阶段崩溃。
	// 因此改为使用受支持的编程式入口：把自定义排除项并入 errcheck.DefaultExcludedSymbols。
	errcheck.DefaultExcludedSymbols = append(errcheck.DefaultExcludedSymbols, parseExcludes(excludesContent)...)
}
