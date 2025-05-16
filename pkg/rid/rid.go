package rid

import "github.com/LiangNing7/goutils/pkg/id"

const (
	// defaultCharset 定义默认的字符集.
	defaultCharset = "abcdefghijklmnopqrstuvwxyz1234567890"

	// defaultIDLength 定义生成的唯一标识符长度.
	defaultIDLength = 6
)

// ResourceID 表示资源的唯一标识符.
type ResourceID string

// String 实现 Stringer 接口.
func (rid ResourceID) String() string {
	return string(rid)
}

// NewResourceID 创建一个新的资源标识符.
func NewResourceID(prefix string) ResourceID {
	return ResourceID(prefix)
}

// New 创建一个带前缀的唯一标识符.
func (rid ResourceID) New(counter uint64) string {
	// 使用自定义选项生成唯一标识符.
	uniqueStr := id.NewCode(
		counter,
		id.WithCodeChars([]rune(defaultCharset)),
		id.WithCodeL(6),
		id.WithCodeSalt(Salt()),
	)
	return rid.String() + "-" + uniqueStr
}
