# log

> 用于 `goutils` 项目的 log 包
>
> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/log
> ```

## Usage

```go
import "github.com/LiangNing7/goutils/pkg/log"

// 设置日志级别
log.Default().LogMode(gormlogger.LogLevel(o.LogLevel))
// 日志的使用
log.Debugw(msg, kvs...)
log.Infow(msg, kvs...)
log.Errorw(err, msg, kvs...)
```

