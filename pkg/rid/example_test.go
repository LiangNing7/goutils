package rid_test

import (
	"fmt"
	"strings"

	"github.com/LiangNing7/goutils/pkg/rid"
)

func ExampleResourceID_String() {
	// 定义一个资源标识符，例如用户资源.
	userRID := rid.NewResourceID("user")
	// 调用 String 方法，将 ResourceID 类型转为字符串类型.
	ridString := userRID.String()

	// 输出结果.
	fmt.Println(ridString)

	// Output:
	// user
}

// ExampleResourceID_New 只断言与机器无关的结构性质（前缀与 code 长度）.
// 具体字符依赖 Salt()（/etc/machine-id 或 hostname），不应硬编码期望值.
func ExampleResourceID_New() {
	// 定义一个资源标识符，例如用户资源.
	userRID := rid.NewResourceID("user")
	generated := userRID.New(1)

	// 输出结果.
	parts := strings.SplitN(generated, "-", 2)
	fmt.Println(parts[0])
	fmt.Println(len(parts[1]))

	// Output:
	// user
	// 6
}
