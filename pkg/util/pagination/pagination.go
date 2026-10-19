package pagination

// GetPageOffset 计算分页查询中数据的起始偏移量（offset）.
// pageNum: 当前页码，从 1 开始.
// pageSize: 每页显示的数据条数.
// 返回值: 当前页对应的偏移量，用于数据库等分页查询.
//
// 边界语义（clamp）：
//   - pageNum < 1 时按第 1 页处理（offset 为 0），避免出现负数；
//   - pageSize < 0 时按 0 处理；
//   - 因此返回值恒 >= 0，不会因为非法输入算出负 offset.
//
// 示例：
// pageNum = 3, pageSize = 10 -> offset = (3 - 1) * 10 = 20.
// pageNum = 0, pageSize = 10 -> offset = 0（旧实现会得到 -10）.
func GetPageOffset(pageNum, pageSize int64) int64 {
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 0 {
		pageSize = 0
	}
	return (pageNum - 1) * pageSize
}
