package pagination

// GetPageOffset 计算分页查询中数据的起始偏移量（offset）.
// pageNum: 当前页码，从 1 开始.
// pageSize: 每页显示的数据条数.
// 返回值: 当前页对应的偏移量，用于数据库等分页查询.
// 示例：
// pageNum = 3, pageSize = 10 -> offset = (3 - 1) * 10 = 20.
func GetPageOffset(pageNum, pageSize int64) int64 {
	return (pageNum - 1) * pageSize
}
