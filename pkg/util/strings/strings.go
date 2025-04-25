package strings

import (
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/asaskevich/govalidator"
)

type frequencyInfo struct {
	s         string
	frequency int
}

type frequencyInfoSlice []frequencyInfo

func (fi frequencyInfoSlice) Len() int {
	return len(fi)
}

func (fi frequencyInfoSlice) Swap(i, j int) {
	fi[i], fi[j] = fi[j], fi[i]
}

func (fi frequencyInfoSlice) Less(i, j int) bool {
	return fi[j].frequency > fi[i].frequency
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
func Unique(ss []string) (result []string) {
	// 使用哈希表去重.
	smap := make(map[string]bool)
	for _, s := range ss {
		smap[s] = true
	}
	// 再把键取出来构成 result 切片.
	for s := range smap {
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

// Reverse 用于反转字符串.
func Reverse(s string) string {
	size := len(s)
	buf := make([]byte, size)
	// 按字节遍历但使用 utf8.DecodeRuneInString 解码出完整的 rune，及其所占字节数 n.
	for start := 0; start < size; {
		r, n := utf8.DecodeRuneInString(s[start:])
		start += n
		// 计算该字符在目标字节切片中的反向位置，用 utf8.EncodeRune 写过去.
		utf8.EncodeRune(buf[size-start:], r)
	}

	// 用 string(buf) 返回反转后的字符串.
	return string(buf)
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
func FrequencySort(list []string) []string {
	// cnt 统计各字符串出现次数.
	cnt := map[string]int{}

	for _, s := range list {
		cnt[s]++
	}

	infos := make([]frequencyInfo, 0, len(cnt))
	for s, c := range cnt {
		infos = append(infos, frequencyInfo{
			s:         s,
			frequency: c,
		})
	}

	sort.Sort(frequencyInfoSlice(infos))

	ret := make([]string, 0, len(infos))
	for _, info := range infos {
		ret = append(ret, info.s)
	}

	return ret
}
