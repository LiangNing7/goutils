# id


> id 生成器.
>
> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/id
> ```

## Code


通过唯一 uint64 生成 short str。

### Usage


```go
import (
	"fmt"
	"github.com/LiangNing7/goutils/pkg/id"
)

func main() {
	fmt.Println(id.NewCode(1))
	fmt.Println(id.NewCode(2))
	fmt.Println(id.NewCode(3))
	fmt.Println(id.NewCode(4))

	fmt.Println(id.NewCode(
		1,
		id.WithCodeChars([]rune{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}),
		id.WithCodeN1(9),
		id.WithCodeN2(3),
		id.WithCodeL(5),
		id.WithCodeSalt(99999),
	))
}
```


### Options


- `WithCodeChars` - code set, 每个字符都将从该 code set 中生成，默认情况下 `['2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'J', 'K', 'L', 'M', 'N', 'P', 'Q', 'R', 'S', 'T', 'V', 'W', 'X', 'Y']`；
- `WithCodeL` - code length，取值范围 `[1, MaxCodeLength]`（`MaxCodeLength = 1024`），`<= 0` 的值会被忽略（保持默认值 8）；
- `WithCodeN1` - 设置 n1 与 `chars.length` 互质 ；
- `WithCodeN2` - 设置 n2 与 `code.length` 互质；
- `WithCodeSalt` - code salt，相同的选项和相同的 uint64 id 将生成相同的代码，可以设置不同的 salt 来生成新的代码。

### 非法配置

- 字符集少于 2 个 rune、code 长度不在 `[1, MaxCodeLength]` 内均属非法配置；
- `NewCode` 遇到非法配置时**退回默认配置**，保证不 panic 且始终返回可用的 code；
- 需要显式感知非法配置时使用 `NewCodeE`，它在配置非法时返回错误：

```go
code, err := id.NewCodeE(1, id.WithCodeChars([]rune{'x'}))
// code == "", err != nil

code, err = id.NewCodeE(1, id.WithCodeL(id.MaxCodeLength+1))
// code == "", err != nil
```

## Snowflake Id


基于 [sonyflake](https://github.com/sony/sonyflake) 生成雪花 ID 

### Usage


```go
import (
	"context"
	"fmt"

	"github.com/LiangNing7/goutils/pkg/id"
)

func main() {
	sf := id.NewSonyflake(
		id.WithSonyflakeMachineId(1),
	)
	if sf.Error != nil {
		fmt.Println(sf.Error)
		return
	}
	fmt.Println(sf.Id(context.Background()))
}
```


### Options


- `WithSonyflakeMachineId` - machine id；
- `WithSonyflakeStartTime` - start time, 设置一次后请勿修改，否则，您可能会得到重复的 ID。
