package rid_test

import (
	"fmt"

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

func ExampleResourceID_New() {
	// 定义一个资源标识符，例如用户资源.
	userRID := rid.NewResourceID("user")
	rid := userRID.New(1)

	// 输出结果.
	fmt.Println(rid)

	// Output:
	// user-b2jxu1
}
