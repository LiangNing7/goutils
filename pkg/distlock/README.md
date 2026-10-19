# 分布式锁 (distlock)

`pkg/distlock` 提供一套统一的分布式锁接口，支持以下后端：

| 后端 | 构造函数 | 备注 |
| --- | --- | --- |
| Noop | `NewNoopLocker` | 仅供测试 / 占位，**跨进程没有任何互斥能力** |
| Redis | `NewRedisLocker` | 推荐 |
| MongoDB | `NewMongoLocker` | |
| MySQL / PostgreSQL / SQLite (GORM) | `NewGORMLocker` | |
| Consul | `NewConsulLocker` | |
| ZooKeeper | `NewZookeeperLocker` | |
| Memcached | `NewMemcachedLocker` | Unlock 非原子，见下文限制 |
| etcd | `NewEtcdLocker` | 基于 `clientv3/concurrency` |

统一接口：

```go
type Locker interface {
	Lock(ctx context.Context) error
	Unlock(ctx context.Context) error
	Renew(ctx context.Context) error
}
```

`Locker` 自始至终**没有变化**，本轮改造全部在接口之后完成。此外每个具体类型都额外实现了：

```go
type Closer interface {
	Close() error
}
```

`Close()` 故意**没有**放进 `Locker`，否则会破坏外部已有的 `Locker` 实现（例如各业务方的测试替身）。

---

## 1. 所有权模型：token + 条件操作

**互斥的正确性来自「每次获取锁时生成的唯一 token」+「条件删除/条件续期」，而不是 ownerID。**

每次 `Lock` 成功时都会生成一个高熵随机 token（`crypto/rand` 驱动的 UUIDv4，见 `newToken()`），并把它和锁一起写进后端：

- `Unlock` 只有在「后端里存的值仍然等于本次获取的 token」时才真正删除；
- `Renew` 同理，只有 token 仍然匹配时才延长租期。

因此「A 的租期过期 → B 拿到锁 → A 恢复后调用 Unlock」不会再删掉 B 的锁，而是返回 `ErrLockNotHeld`。

`ownerID`（`WithOwnerID`）现在**只用于日志**，默认值是 `hostname-pid`。它不参与任何所有权判断，所以同机多进程 / 同 hostname 容器不会互相「假成功」。

### 错误约定

| 哨兵错误 | 何时返回 |
| --- | --- |
| `ErrLockAlreadyHeld` | `Lock` 时锁被别人（或本实例自己）持有 |
| `ErrLockNotHeld` | `Unlock`/`Renew` 时本实例并不持有锁：从未获取过、已经释放过，或租期过期后已被他人接管（此时**不会**动别人的锁） |
| `ErrInvalidLockTimeout` | 构造函数收到小于 `MinLockTimeout` 的超时（仅对返回 error 的构造函数） |

全部用 `%w` 包装，请用 `errors.Is` 判断。

`Unlock` 是幂等的：重复调用不会 panic、不会误删，只会返回 `ErrLockNotHeld`。

### 破坏性行为变更：同一个实例重复 Lock

对同一个 locker 实例，在已持锁的情况下再次 `Lock` 会返回 `ErrLockAlreadyHeld`。
旧实现（Redis 后端）在这种情况下会返回 nil。这样做的目的是避免一次获取对应两个 token。

---

## 2. 续期模型与 context 的关系

每次成功获取锁都会启动一个后台 goroutine 周期性调用 `Renew`。两点关键设计：

1. **续期 goroutine 与调用方 ctx 解耦**。它运行在 `context.WithoutCancel(ctx)` 派生出的 ctx 上。因此下面这种最常见的写法

   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   if err := locker.Lock(ctx); err != nil { ... }
   ```

   在 `Lock` 返回后 ctx 被取消，**不会**杀掉续期协程（旧实现会，导致锁静默过期）。

2. **ticker 是 goroutine 的局部变量，通过 per-acquisition channel 停止并 join**。这消除了两个已复现的缺陷：
   - 旧实现中 `Unlock` 写 `l.renewTicker = nil`，续期协程无锁读 `l.renewTicker.C`；`-race` 报 `WARNING: DATA RACE`，并且因为 `select` 会先求值所有 channel 操作数，只要 `Unlock` 先于协程首次调度就必然 `SIGSEGV`；
   - 旧实现从不 `close(stopChan)`，且 `Ticker.Stop()` 不关闭 channel，`case <-l.stopChan` 是死代码：`200 × (Lock+Unlock)` 泄漏 200 个 goroutine。

`Unlock`、`Close` 都会停止并 join 该协程；两者都幂等（`renewManager.stop` 在互斥锁下清空状态，第二次调用直接返回，不会 double-close panic）。

### 各后端的续期方式

| 后端 | 续期方式 |
| --- | --- |
| Redis | Lua `if get==token then pexpire` |
| Mongo / GORM | `UPDATE ... WHERE name/token = ...`（条件更新） |
| Memcached | `GET`(gets) → 比对 token → `CAS`（**真原子**；token 不符或 CAS 冲突返回 `ErrLockNotHeld`） |
| Consul | `Session().Renew(sessionID)`，用的是存下来的 **sessionID**（旧实现传的是 hostname，必然失败） |
| ZooKeeper | **无续期**：临时节点靠会话存活。`Renew` 只校验节点是否仍属于自己，不再打印假的 "Lock renewed" 日志 |
| etcd | **无续期 goroutine**：`concurrency.Session` 自带 lease keepalive。`Renew` 只检查 lease 是否还活着 |

---

## 3. `Close()` 的使用

`Close()` 停止续期协程并释放 locker 自己持有的资源：

| 后端 | `Close()` 做什么 | 是否顺带释放锁 |
| --- | --- | --- |
| Redis / GORM / Memcached / Noop | 停止续期（client / db 由调用方持有，**不会被关闭**） | 否 |
| MongoDB | 停止续期 + `Disconnect()`（client 是本 locker 建的） | 否 |
| ZooKeeper | 停止续期 + `conn.Close()`（会话结束 → 临时节点消失） | **是** |
| Consul | 停止续期 + `Destroy(sessionID)`（Behavior=release → key 释放） | **是** |
| etcd | 停止续期 + `session.Close()`(Revoke lease) + `cli.Close()` | **是** |

`Close()` 幂等。如果只是想释放锁，请先 `Unlock` 再 `Close`。

---

## 4. 超时校验

- `MinLockTimeout = 1s`。原因：多个后端是秒粒度（Memcached 的 expiration、etcd lease），且续期间隔为 `lockTimeout/2`。
- **返回 error 的构造函数**（Mongo / etcd / Consul / ZooKeeper / GORM）在超时非法时返回 `ErrInvalidLockTimeout`。
- **不返回 error 的构造函数**（Redis / Memcached / Noop）做 clamp + 打 warning 日志，绝不把非法值丢给 `time.NewTicker`（旧实现在 `WithLockTimeout(0)` 时会 `panic: non-positive interval for NewTicker`）。
- Memcached 额外把 expiration 上限压到 `30天-1秒`：memcached 把大于 30 天的值当作绝对 unix 时间戳，否则会写成一个 1970 年的过期时间（等于永不过期）。
- Consul 额外要求 `10s ~ 86400s`（服务端限制），见下文。

---

## 5. 各后端的真实保证与已知限制

### Redis
- 用 `SET NX` + 两个 Lua 脚本（compare-and-delete / compare-and-expire），单实例上原子。
- Cluster 下 key 落在单个 slot，脚本仍然原子；但 Redis 复制是异步的，failover 可能丢锁（Redlock 的经典注意事项）。
- 客户端由调用方提供，`Close()` 不会关闭它，一个 client 可以给多个 locker 共用。

### MongoDB
- 锁文档以 `_id = 锁名` 存储，唯一性由 `_id` 天然保证（旧实现依赖一个**没有索引**的 `name` 字段，两个并发进程可以同时"成功"）。
- 获取锁是**单条原子** `UpdateOne(upsert)`：filter 为 `{_id, $or: [{expiredAt: {$lte: now}}, {token: {$exists: false}}]}`，靠 `MatchedCount`/`UpsertedCount` 判定胜负；锁被占有时 upsert 退化为插入 `{_id: name}` 并返回 duplicate key，被翻译成 `ErrLockAlreadyHeld`。
- 旧实现用 `$set` 与 `$setOnInsert` 给同一批字段赋值，MongoDB 会报 "would create a conflict"；现在只用 `$set`。
- 租期比较用的是**客户端时钟**，因此精度取决于各主机之间的时钟偏差，请确保 NTP。

### GORM (MySQL / PostgreSQL / SQLite)
- 表新增 `owner_token` 列（`AutoMigrate` 自动加列）。
- 获取锁拆成两条**互相独立**的语句：先 `UPDATE ... WHERE name=? AND (expired_at<=? OR owner_token IS NULL OR owner_token='')` 抢过期行，`RowsAffected` 即胜负；抢不到再 `INSERT ... ON CONFLICT DO NOTHING`（`clause.OnConflict{DoNothing:true}`），据 `RowsAffected` 判断是否创建成功。
  - 不用 `SELECT ... FOR UPDATE`，也就不再存在「两个事务都读到过期行、都 Save、都成功」的窗口。
  - 用 `DO NOTHING` 且不共用事务，避免重复键错误把 PostgreSQL 事务打成 aborted（25P02），旧实现在 PG 上的抢占路径是根本走不通的。

### Consul
- 每次获取锁创建 session（`Behavior=release`），用 `KV().Acquire(pair)` 并**检查返回的 bool**（旧实现丢弃了 bool，所以每次 Lock 都"成功"）。
- `Renew` 用存下来的 sessionID；`Unlock` 先 `Release` 再 `Destroy(sessionID)`。若 `Lock` 竞争失败，会销毁自己刚创建的 session，不留垃圾。
- **lock timeout 必须是 10s ~ 86400s**：Consul 服务端只接受这个区间的 session TTL（Session HTTP API: "between 10s and 86400s"），区间外的值由构造函数直接返回 `ErrInvalidLockTimeout`，而不是等到 `Lock` 时才发现。因此 **Consul 后端无法实现小于 10 秒的锁超时**。TTL 会向上取整到整秒。

### ZooKeeper
- 锁是**一个共享路径上的临时节点**。旧实现是 `Create("<lockPath>/<ownerID>", ..., flags=0)`：路径带 ownerID 导致不同 owner 永不相撞（**零互斥**），且是持久节点，进程崩溃后锁永不释放。
- 路径强制以 `/` 开头（旧实现的默认锁名 `onex-distributed-lock` 没有前导 `/`，ZK 直接报错），缺失的父路径会被自动创建。
- `Unlock` 先 `Get` 读回数据比对 token，再用 `Get` 返回的 version 做 CAS 删除。
- `Renew` 是空操作（临时节点不需要续期），只校验节点是否仍属于自己。

### Memcached
- **`Unlock` 不是原子的**：memcached 没有 compare-and-delete，实现是 `GET` 比对 token 后再 `DELETE`，两次往返之间存在竞态窗口——如果你的租期正好在这个窗口内过期且别人拿到了锁，`DELETE` 会删掉**别人**的锁。该窗口只有一个往返宽，且只在"租期本来就要过期"时才可能打开，但它是真实存在的，用 memcached 协议无法关闭。
- `Renew` **是原子的**：`Get`（发出的是 `gets`，带回 CAS id）比对 token 后 `CompareAndSwap`，CAS 失败即 `ErrLockNotHeld`。
- 因为上述原因，对互斥正确性有要求的场景请优先选 Redis / etcd / Consul，把 memcached 后端当作 best effort。

### etcd
- 基于 `go.etcd.io/etcd/client/v3/concurrency` 的 `Session` + `Mutex`（同模块已依赖），这是 etcd 官方的锁原语：
  - `Session` 持有 TTL = 锁超时的 lease，由客户端库心跳续期；
  - `Mutex` 是 `<lockKey>/` 前缀下、绑定 lease 的 key，所有权由 create revision 决定，`TryLock` 返回 `concurrency.ErrLocked`（保持非阻塞语义）；
  - `Unlock` 删除的是本 session 独有的 key，**结构上不可能**删到别人的锁。
- 旧实现是裸 `Put`，无条件覆盖且返回 nil，即任何并发都"成功"（零互斥）。
- 取舍：因为把所有权委托给了 `concurrency` 包（其 `Session`/`Mutex` 包装的是具体的 `*clientv3.Client`），这个后端**没有可注入的测试缝**，本次改动**没有**在真实 etcd 上验证过（详见测试覆盖表）。

### Noop
- 不接触任何后端，但**不再是无状态的 no-op**：它完整模拟本地生命周期（token、重复 Lock 拒绝、续期记账），是本包测试套件的参考实现。
- 它**不提供任何跨进程互斥**，生产环境请勿使用。

---

## 6. 测试覆盖情况（重要）

本轮改造的验证证据全部来自 `pkg/distlock/*_test.go`，运行方式：

```bash
go test -race ./pkg/distlock/...
```

| 后端 | 验证方式 | 覆盖到什么程度 |
| --- | --- | --- |
| Noop | 无后端依赖 | **完整**：生命周期、panic 时序、goroutine 泄漏、幂等、非法超时、ctx 取消后仍续期 |
| Redis | 内存假实现（模拟 `SET NX`/`GET` 与两个 Lua 脚本的语义） | 逻辑**已测**：所有权、抢占后不误删、续期延长 TTL、外来持锁者不被误认；Lua 脚本本身用字符串断言守护。**未在真实 Redis 服务端验证** |
| MongoDB | 内存假 collection（模拟生成的 filter/update/upsert 语义，含 duplicate key 与 `$set`/`$setOnInsert` 冲突） | 逻辑**已测**：首次获取、活跃锁不被覆盖、过期抢占、条件续期。**未在真实 MongoDB 验证** |
| GORM | **真实 SQLite（纯 Go 驱动，已在依赖图中）** | 获取/续期/所有权/24 路并发抢锁都跑在**真实数据库**上；`owner_token` 加列（滚动升级）已测。**MySQL 的 `ON DUPLICATE KEY` 与 PostgreSQL 的 `ON CONFLICT` 分支未在真实 MySQL/PG 服务端验证** |
| Consul | 内存假 KV/Session（模拟会话有效期与 `Acquire` 的 bool 语义） | 逻辑**已测**：bool 被检查、用 sessionID 续期、Unlock 释放并销毁 session、TTL 秒级向上取整、stale session 不误删。**未在真实 Consul 验证** |
| ZooKeeper | 内存假连接（模拟路径校验、`ErrNodeExists`、version CAS） | 逻辑**已测**：路径绝对值化、临时节点、共享路径、CAS 删除、Renew 不再说假话。**未在真实 ZK 集群验证** |
| Memcached | 内存假客户端（模拟 `Add`/`GET`(gets+CasID)/`CAS`/`Delete`） | 逻辑**已测**：非零过期、CAS 续期、驱逐容错、不删他人 key。**未在真实 memcached 验证** |
| etcd | 无 | **完全未验证**。只有构造函数参数校验（`ErrInvalidLockTimeout`）有测试，其余仅通过编译。需要一个真实 etcd 集群才能测试 |

> 说明：对 Redis / Mongo / Consul / ZK / Memcached，假实现只保证**本包发出的命令序列正确**（例如 Acquire 的 bool 被检查、token 被比对）。后端自身的原子性由各后端的协议保证，假实现不构成对后端原子性的验证。**真实数据库上的并发仲裁只在 GORM + SQLite 上跑过**。

### 已知限制 / 尚未验证清单

1. etcd 后端**完全没有**功能验证（见上表）。
2. MySQL / PostgreSQL 真实服务端上的抢占与 `ON CONFLICT` 行为未验证（逻辑在 SQLite 上等价验证）。
3. Redis Cluster / Sentinel failover 期间的锁丢失、Consul 多数据中心、ZK 集群选举期间的会话行为，均未验证。
4. Memcached 的 `Unlock` 非原子窗口（见 §5），是设计限制而非缺陷。
5. Mongo 的租期判断依赖客户端时钟，未做时钟偏差下的行为测试。
6. 各后端的 `Lock` 都是非阻塞的（抢不到立即返回 `ErrLockAlreadyHeld`），没有实现阻塞等待 / 公平队列（etcd 的 `concurrency.Mutex.Lock` 支持排队，但为了保持接口语义一致只用了 `TryLock`）。

---

## 7. 破坏性变更清单

1. **行为**：`Unlock`/`Renew` 在不持锁时返回 `ErrLockNotHeld`（此前多数后端返回 nil 或后端原始错误）。
2. **行为**：`Lock` 在锁被占用时统一返回 `ErrLockAlreadyHeld`（可 `errors.Is` 判断）；此前各后端返回 `fmt.Errorf` 字符串，etcd 甚至返回 nil。
3. **行为**：同一实例重复 `Lock` 返回 `ErrLockAlreadyHeld`（Redis 后端此前返回 nil）。
4. **构造**：Mongo / etcd / Consul / ZooKeeper / GORM 的构造函数在 `lockTimeout < 1s` 时**返回 error**；Redis / Memcached / Noop 会 clamp 到 1s 并打 warning。
   **Consul 还额外要求 `10s <= lockTimeout <= 86400s`**（Consul 服务端的 session TTL 限制），因此 Consul 后端不再支持小超时。默认值（10s）不受影响。
5. **接口**：新增 `Closer` 接口 + 各具体类型的 `Close() error`；`Locker` 接口**未变**。
6. **新增导出**：`ErrLockAlreadyHeld`、`ErrLockNotHeld`、`ErrInvalidLockTimeout`、`MinLockTimeout`、`Closer`。
7. **GORM 表结构**：`locks` 表新增 `owner_token` 列（`AutoMigrate` 自动加列）。旧行 `owner_token` 为空，被视为"未认领"，第一次 `Lock` 会直接接管该行，无需人工迁移。
8. **Mongo 文档结构**：锁文档改为以 `_id = 锁名` 存储；旧版遗留的 `{_id: ObjectId, name: ...}` 文档不再被读取，可以手动清理（不清理也不影响正确性，只是残留垃圾）。
9. **ZooKeeper 节点**：锁路径从 `<lockPath>/<ownerID>`（持久）改为 `<lockPath>`（临时）。旧的持久节点不会被新实现使用，建议手动删除；`lockPath` 现在强制要求以 `/` 开头（缺失会自动补 `/`）。
10. **Redis**：续期从 `EXPIRE`（秒）改为 `PEXPIRE`（毫秒），亚秒级超时的精度不再被截断。
11. **Consul**：session TTL 由 `fmt.Sprintf("%s")` 改为向上取整的整数秒；`Unlock`/`Close` 会销毁 session。
12. **Memcached**：expiration 由 `int32(timeout.Seconds())` 改为向上取整并夹在 `[1s, 30天-1秒]`。
13. **etcd**：改用 `concurrency.Session` + `Mutex`，因此 `Lock` 变成真正的排他获取（旧实现无条件覆盖）。
14. **Noop**：不再是"永远成功"的空实现，现在会记状态、拒绝重复 Lock。
15. 默认 `ownerID` 从 `hostname` 变为 `hostname-pid`（仅影响日志）。

---

## 8. 示例（examples/）

`examples/` 下 4 个 `main` 包。**凭据一律从环境变量读取，仓库内不再有任何内网地址和口令**。

```bash
# Redis
ONEX_REDIS_ADDR=127.0.0.1:6379 go run ./pkg/distlock/examples/redis

# MySQL（密码必填，没有安全默认值）
ONEX_MYSQL_ADDR=127.0.0.1:3306 ONEX_MYSQL_PASSWORD=secret \
    go run ./pkg/distlock/examples/mysql

# PostgreSQL（密码必填）
ONEX_POSTGRES_ADDR=127.0.0.1:5432 ONEX_POSTGRES_PASSWORD=secret \
    go run ./pkg/distlock/examples/postgresql

# Noop（不连接任何后端）
go run ./pkg/distlock/examples/noop
```

| 示例 | 环境变量 | 默认值 |
| --- | --- | --- |
| redis | `ONEX_REDIS_ADDR` / `ONEX_REDIS_USERNAME` / `ONEX_REDIS_PASSWORD` / `ONEX_REDIS_DATABASE` | `127.0.0.1:6379` / 空 / 空 / `0` |
| mysql | `ONEX_MYSQL_ADDR` / `ONEX_MYSQL_USERNAME` / `ONEX_MYSQL_PASSWORD` / `ONEX_MYSQL_DATABASE` | `127.0.0.1:3306` / `root` / **必填** / `onex` |
| postgresql | `ONEX_POSTGRES_ADDR` / `ONEX_POSTGRES_USERNAME` / `ONEX_POSTGRES_PASSWORD` / `ONEX_POSTGRES_DATABASE` | `127.0.0.1:5432` / `postgres` / **必填** / `postgres` |

---

## 9. 用法示例

```go
locker := distlock.NewRedisLocker(client, distlock.WithLockName("my-job"))
defer locker.Close() // 停掉续期协程（client 由调用方持有，不会被关闭）

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := locker.Lock(ctx); err != nil {
	if errors.Is(err, distlock.ErrLockAlreadyHeld) {
		// 别人正在跑，直接退出
		return
	}
	return err
}
// ctx 现在可以随便 cancel；续期运行在与 ctx 解耦的 context 上

... // 业务逻辑

// Unlock 只会在后端里仍存着本次 token 时生效；
// 如果租期已经过期且锁被别人接管，会返回 ErrLockNotHeld 并且不动别人的锁。
if err := locker.Unlock(context.Background()); err != nil &&
	!errors.Is(err, distlock.ErrLockNotHeld) {
	return err
}
```
