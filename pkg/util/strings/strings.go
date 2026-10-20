package strings

import (
	"slices"
	"sort"
	"strings"

	"github.com/asaskevich/govalidator"
)

type frequencyInfo struct {
	s         string
	frequency int
	// order 是该字符串在输入中首次出现的下标，用于在频次相同时提供确定性次序.
	order int
}

// Diff 求差集，返回 base 中存在，但 exclude 中不存在的元素集合.
func Diff(base, exclude []string) (result []string) {
	// 先将 exclude 切片的所有元素插入 map.
	excludeMap := make(map[string]bool)
	for _, s := range exclude {
		excludeMap[s] = true
	}
	// 遍历 base，过滤掉哈希表中存在的元素.
	for _, s := range base {
		if !excludeMap[s] {
			result = append(result, s)
		}
	}
	return result
}

// Include 求交集，返回同时出现在 base 和 include 两个切片中的元素.
func Include(base, include []string) (result []string) {
	// 先把 base 的元素做哈希映射.
	baseMap := make(map[string]bool)
	for _, s := range base {
		baseMap[s] = true
	}
	// 再扫描 include，只收集映射中存在的项.
	for _, s := range include {
		if baseMap[s] {
			result = append(result, s)
		}
	}
	return result
}

// Unique 去重，从一个切片中去除重复元素，只保留每种字符串一次.
//
// 行为变更：输出顺序固定为「首次出现顺序」，不再依赖 map 迭代顺序，
// 因此同一输入在多次运行中结果一致（旧实现顺序随机）。
func Unique(ss []string) (result []string) {
	// 使用哈希表去重，同时按首次出现顺序收集，保证输出确定性.
	smap := make(map[string]bool)
	for _, s := range ss {
		if smap[s] {
			continue
		}
		smap[s] = true
		result = append(result, s)
	}
	return result
}

// CamelCaseToUnderscore 驼峰转下划线：MyVariableName => my_variable_name.
func CamelCaseToUnderscore(str string) string {
	return govalidator.CamelCaseToUnderscore(str)
}

// UnderscoreToCamelCase 下划线转驼峰：my_variable_name => MyVariableName.
func UnderscoreToCamelCase(str string) string {
	return govalidator.UnderscoreToCamelCase(str)
}

// FindString 在 array 中查找 str，返回首次出现的下标；找不到返回 -1.
func FindString(array []string, str string) int {
	for index, s := range array {
		if str == s {
			return index
		}
	}

	return -1
}

// StringIn 判断某字符串是否在切片中。内部直接调用 FindString.
func StringIn(str string, array []string) bool {
	return FindString(array, str) > -1
}

// Reverse 按 rune 反转字符串.
//
// 语义：
//   - 合法 UTF-8：按 rune 反转，多字节字符不会被拆散（"中文abc" -> "cba文中"）；
//   - 非法 UTF-8 字节序列：Go 的 rune 解码会把非法字节规整为 U+FFFD (unicode.ReplacementChar)，
//     因此输出是确定性的，且保证不会 panic（旧实现会对非法输入切片越界 panic）。
//
// 该语义适用于来自日志 / CSV / 外部协议等不可控输入。
func Reverse(s string) string {
	// []rune 转换会把非法字节规整为 U+FFFD，随后原地反转即可.
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	return string(runes)
}

// Filter 从 list 中移除所有等于 strToFilter 的元素，返回过滤后的新 slice.
func Filter(list []string, strToTilter string) (newList []string) {
	for _, item := range list {
		if item != strToTilter {
			newList = append(newList, item)
		}
	}

	return newList
}

// Add 向切片中添加一个元素，但若该元素已存在则不重复添加，保持唯一性.
func Add(list []string, str string) []string {
	if slices.Contains(list, str) {
		return list
	}

	return append(list, str)
}

// Contains 与 StringIn 等价，也用于判断是否包含指定字符串.
func Contains(list []string, strToSearch string) bool {
	return slices.Contains(list, strToSearch)
}

// ContainsEqualFold 忽略大小写地判断切片中是否有某字符串.
func ContainsEqualFold(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// FrequencySort 将输入切片中的字符串，按出现次数从高到低排序后返回唯一的字符串列表.
//
// 行为变更：频次相同的元素按「首次出现顺序」排列，使输出完全确定，
// 不再依赖 map 迭代顺序（旧实现在频次相同时顺序随机，同一输入多次运行结果不同）。
func FrequencySort(list []string) []string {
	// cnt 统计各字符串出现次数；firstIdx 记录首次出现的下标，用于稳定排序.
	cnt := map[string]int{}
	firstIdx := map[string]int{}

	for i, s := range list {
		if _, ok := firstIdx[s]; !ok {
			firstIdx[s] = i
		}
		cnt[s]++
	}

	infos := make([]frequencyInfo, 0, len(cnt))
	for s, c := range cnt {
		infos = append(infos, frequencyInfo{
			s:         s,
			frequency: c,
			order:     firstIdx[s],
		})
	}

	// 先按频次降序，频次相同时按首次出现顺序升序，保证结果确定.
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].frequency != infos[j].frequency {
			return infos[i].frequency > infos[j].frequency
		}
		return infos[i].order < infos[j].order
	})

	ret := make([]string, 0, len(infos))
	for _, info := range infos {
		ret = append(ret, info.s)
	}

	return ret
}
