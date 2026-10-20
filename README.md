# goutils

`goutils` 是一个轻量、实用的 Go 工具库集合，面向日常服务端开发：CLI 应用骨架、HTTP/gRPC 服务封装、日志、配置、数据库、分布式锁、鉴权、通用工具等。

- 模块路径：`github.com/LiangNing7/goutils`
- 语言版本：Go 1.24（见 `go.mod`）
- 许可证：[MIT](./LICENSE)

> **重要说明**：本仓库目前处于「评审与加固」阶段。**多数包没有测试覆盖**，部分包（如 `pkg/distlock`）正在重写。下表的 `稳定性` 列如实标注了每个包的状态，请勿把 `unverified` / `experimental` 的包当作生产可用。详见 [状态与成熟度](#状态与成熟度)。

## 安装

```bash
go get github.com/LiangNing7/goutils@latest
```

按需导入具体子包，例如：

```go
import (
    "github.com/LiangNing7/goutils/pkg/ptr"
    "github.com/LiangNing7/goutils/pkg/id"
)
```

## 最小示例

`ptr` 是无外部依赖的泛型指针工具包（Go 1.18+ 泛型）：

```go
package main

import (
    "fmt"

    "github.com/LiangNing7/goutils/pkg/ptr"
)

type Config struct {
    Retries *int
}

func main() {
    // 生成指针：To 返回指向 v 的指针.
    c := Config{Retries: ptr.To(3)}

    // 取值：From 在指针为 nil 时返回零值，FromOr 返回指定的默认值.
    fmt.Println(ptr.From(c.Retries))           // 3
    fmt.Println(ptr.From[int](nil))            // 0
    var missing *int
    fmt.Println(ptr.FromOr(missing, 10))       // 10

    // 判空与比较.
    fmt.Println(ptr.IsNil(missing))            // true
    fmt.Println(ptr.Equal(ptr.To(1), ptr.To(1))) // true

    // 映射：把 *int 映射为 *string.
    s := ptr.Map(ptr.To(42), func(v int) string { return fmt.Sprint(v) })
    fmt.Println(ptr.FromOr(s, ""))             // "42"
}
```

另一个零依赖的例子是 `id`（唯一 ID 生成）：

```go
sf := id.NewSonyflake(id.WithSonyflakeMachineId(1))
uid := sf.Id(context.Background()) // uint64，全局唯一
```

## 包索引

共 23 个顶层条目（22 个 `pkg/` 下的包 + 仓库根的 `addlicense` 工具）。

`稳定性` 含义：

| 取值 | 含义 |
| --- | --- |
| `stable` | 接口相对稳定、逻辑简单，且**有基本单元测试**覆盖；不代表生产级 SLA |
| `unverified` | **没有（或几乎没有）测试**，依赖成熟但行为未经本仓库验证，使用前请自行评估 |
| `experimental` | 已知存在设计/语义问题或正在重写，接口可能变动，**不建议**用于新项目 |

| 包 | 用途 | 稳定性 |
| --- | --- | --- |
| [`app`](./pkg/app) | 基于 cobra + viper 的 CLI 应用骨架（Kubernetes 风格），统一 flag/配置处理、帮助与健康检查 | `unverified` |
| [`authn`](./pkg/authn) | 认证抽象：`IToken`/`Authenticator` 接口与 bcrypt 加密/校验；含 `authn/jwt` 子包（JWT 签发解析 + Storer） | `unverified` |
| [`authz`](./pkg/authz) | 基于 Casbin 的授权器（ACL 模型、策略自动加载、Wire ProviderSet） | `unverified` |
| [`cli`](./pkg/cli) | CLI 脚手架辅助，目前提供 `cli/genericclioptions`（IOStreams、命令头等） | `unverified` |
| [`core`](./pkg/core) | 通用 Web 层：gin 请求绑定/校验/统一错误响应，以及基于 copier 的结构体深拷贝 | `unverified` |
| [`db`](./pkg/db) | 创建 MySQL / PostgreSQL / Redis 实例的辅助函数，附 GORM trace 插件 | `unverified` |
| [`distlock`](./pkg/distlock) | 分布式锁（GORM / Redis / Etcd / Zookeeper / Consul / Memcached / MongoDB / Noop） | `experimental` |
| [`errorsx`](./pkg/errorsx) | 统一错误类型 `ErrorX`，含 gRPC status 映射与 `errors.Is/As/Unwrap` 包装 | `stable` |
| [`i18n`](./pkg/i18n) | 基于 nicksnyder/go-i18n 的国际化，支持通过 `context` 传递语言 | `stable` |
| [`id`](./pkg/id) | 唯一 ID 生成：可定制字符集的混淆码 `NewCode` 与 `Sonyflake` | `stable` |
| [`log`](./pkg/log) | 基于 zap 的日志实现：`Logger` 接口、`Options`，含 gorm / kratos 适配 | `unverified` |
| [`logger`](./pkg/logger) | 极简 `Logger` 接口定义（含 `empty`、`onex` 两个实现） | `unverified` |
| [`options`](./pkg/options) | Kubernetes 风格的服务端配置选项：HTTP/gRPC/TLS/Etcd/Consul/Kafka/JWT/健康检查/Jaeger 等 | `unverified` |
| [`ptr`](./pkg/ptr) | 泛型指针工具：`To`/`From`/`FromOr`/`IsNil`/`Clone`/`CloneBy`/`Equal`/`Map` 等 | `stable` |
| [`rid`](./pkg/rid) | 资源 ID 生成（前缀 + 机器指纹 + 随机盐），并支持 `Salt`/`ReadMachineID` | `stable` |
| [`server`](./pkg/server) | HTTP / GRPC / gRPC-Gateway / Kratos 服务器封装与统一优雅退出（`Serve`） | `unverified` |
| [`store`](./pkg/store) | 泛型 GORM 存储层，含 `where` 查询构造、`registry` 自动迁移、logger 实现 | `unverified` |
| [`token`](./pkg/token) | JWT 令牌签发与解析，支持从 gin / gRPC 请求上下文提取 token | `stable` |
| [`util`](./pkg/util) | 通用工具集合：`file`/`gen`/`ip`/`lint`/`pagination`/`reflect`/`retry`/`strings`/`version`/`controller` | `stable`（子包覆盖不均，`lint/*` 无测试） |
| [`validation`](./pkg/validation) | 基于反射的结构体字段校验：必填字段检查、`Rules` 规则化校验与自定义校验函数 | `unverified` |
| [`version`](./pkg/version) | 构建信息（如 git 版本）打印与 flag 绑定，Kubernetes component-base 风格 | `stable` |
| [`watch`](./pkg/watch) | 基于 robfig/cron 的定时任务管理：`manager`/`registry`/`initializer`/logger | `unverified` |
| [`addlicense`](./addlicense)（工具） | 为源文件批量添加/校验 license header 的命令行工具 | `stable` |

## 状态与成熟度

本库**不是**一个经过验证的生产级组件集，请按下表谨慎选用：

- **测试覆盖极度不足**：评审开始时全仓仅约 20 个测试文件，且集中在少数包（`ptr`、`rid`、`id`、`token`、`errorsx`、`i18n`、`version`、`util/*` 等）；而 `cli`、`db`、`logger`、`server`、`validation`、`watch` 等包**完全没有测试文件**。（本分支正在并行补充测试，数量持续增长，请以仓库最新 `go test ./...` 结果为准。）
- **`pkg/distlock` 标注为 `experimental`**：该包正在被整体重写，现有实现存在锁语义缺陷（获取/续期/释放的边界条件），接口可能变动。除 `Noop` 外，MySQL/PostgreSQL/Redis 之外的后端（Etcd/Zookeeper/Consul/Memcached/MongoDB）在包内 README 中即声明为「未测试」。
- **`pkg/authz` 曾长期编译失败**：自 2025-08-11 起该包无法编译，直到本次评审才修复——这正是本仓库新增 CI（`.github/workflows/ci.yml`）的直接动因。
- **源文件普遍缺少 license header**：评审时 `grep -rl '^// Copyright' --include='*.go' .` 命中 **0 个**（`.go` 文件无一带有版权头）。`addlicense` 工具的存在暗示本应有统一 header，但目前并未落地（详见下方建议）。
- **包功能存在重叠**：见「重叠与取舍」小节。

### 重叠与取舍

以下包对功能相近但定位不同，**暂不合并**（合并会破坏已导出的 API），使用前请明确差异：

| 包对 | 区别 | 建议 |
| --- | --- | --- |
| `version` vs `util/version` | `version` 输出**构建信息**（git 版本等，k8s component-base 风格）；`util/version` 是**语义化版本号解析/比较**（semver） | 二者职责不同，保留；文档中明确区分即可 |
| `log` vs `logger` | `log` 是 **zap 的具体实现**（含 Options、gorm/kratos 适配）；`logger` 只是**接口定义** + 空/onex 实现 | `logger.Logger` 被 `store`/`watch` 引用，作为依赖倒置接口保留；`log` 作为实现保留 |
| `store/{registry,logger}` vs `watch/{registry,logger}` | `store` 的是**数据模型注册/迁移**与存储日志；`watch` 的是**任务注册表**与定时任务日志 | 语义不同、分属两套领域，保留 |

## 建议的后续动作（按优先级）

1. **P0**：让 CI 在主干上保持绿色；为 `distlock` 重写补齐并发测试（并跑 `-race`）。
2. **P0**：为 `authz`、`store`、`watch`、`core` 等核心包补充最小单元测试。
3. **P1**：统一 license header。建议在确认版权署名后执行：
   ```bash
   go run ./addlicense -v -l mit -c LiangNing7 -y 2025 \
     --skip-dirs '(^|/)(\.git|node_modules|testdata)$' \
     --skip-files '\.pb\.go$' .
   ```
   （先用 `--check` 模式预览将被修改的文件；本仓库当前**尚未**批量添加 header。）
4. **P2**：把 `addlicense/.golangci.yaml` 提炼为根目录 lint 配置并接入 CI——该文件约 25KB、规则较严，直接复制到根目录可能导致全仓大面积告警，需分阶段放宽。

## License

本项目采用 [MIT License](./LICENSE)。版权归 LiangNing7 所有。
