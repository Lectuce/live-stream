# 自动压测与运行时问题修复记录

记录日期：2026-08-31

## 1. 变更范围

本记录覆盖从加入自动压测脚本到首次完整通过端到端测试期间的代码修改，主要对应以下提交：

| 提交 | 说明 |
|---|---|
| `95fc6468f4a207be3d7cd07a829d856d73f6212f` | 加入自动压测脚本、压测文档并修复注册错误处理 |
| `7bb9a1c6b3613a0ccb30e4ffc4d07a6b88533ed2` | 修复房间创建时的零时间写入和错误响应 |
| `47ac3fe92d7aadef0783d1211d2078acb71c49d4` | 修复 WebSocket channel 关闭竞态和 PowerShell stderr 问题 |

## 2. Windows 自动压测脚本

文件：`scripts/load-test.ps1`

新增了从接口准备到压测清理的自动化流程：

```text
go test / go vet
→ 检查后端连通性
→ 自动注册测试用户
→ 登录并提取 sid
→ 验证 Session
→ 创建房间并开播
→ 运行 WebSocket benchmark
→ 检查历史弹幕
→ 自动关播
```

脚本的主要行为如下：

- 默认连接 `http://127.0.0.1:19000` 和 `ws://127.0.0.1:19000/api/v1/ws`；
- 默认创建 10 个 WebSocket 连接，运行 30 秒；
- 每次生成唯一的测试用户名和邮箱，避免重复注册；
- 自动解析登录响应中的 `sid`；
- 支持复用手动建立的 SSH 隧道；
- 配置 SSH 密钥后，可通过 `-StartTunnel` 自动建立和关闭隧道；
- benchmark 结束后解析 `[FINAL]` 指标；
- 要求 `errors=0`，发送模式下要求 `sent>0`、`received>0`；
- 查询历史消息，确认存在 `bench-*` 弹幕；
- 使用 `finally` 自动停止直播并关闭由脚本启动的 SSH 隧道；
- API 失败时输出请求方法、URL、状态码和响应内容。

### 2.1 Session Cookie 修复

Windows PowerShell 5.1 对手工设置的 `Cookie` Header 处理不稳定，曾导致 `/auth/me` 返回“未登录”。脚本改为使用：

```powershell
Microsoft.PowerShell.Commands.WebRequestSession
System.Net.Cookie
```

脚本把 `sid` 放入 Cookie 容器，并仅在本地 HTTP SSH 隧道客户端中将该 Cookie 标记为非 Secure。后端浏览器 Cookie 的 Secure 设置没有被修改。

### 2.2 NativeCommandError 修复

Go 的 `log` 包默认把正常 benchmark 日志写到 stderr。Windows PowerShell 5.1 在全局 `$ErrorActionPreference = "Stop"` 时会把这些日志包装为 `NativeCommandError`，造成脚本提前进入清理流程。

现在只在执行 benchmark 期间临时使用：

```powershell
$ErrorActionPreference = "Continue"
```

benchmark 结束后立即恢复原值，并继续使用 `$LASTEXITCODE` 判断真正的进程失败。

## 3. 注册链路错误处理

### 3.1 `internal/repo/user.go`

`SelectUserByEmail` 现在单独处理：

```go
errors.Is(err, gorm.ErrRecordNotFound)
```

新邮箱不存在是注册过程的正常情况，因此返回 `(nil, nil)`，而不是数据库错误。

### 3.2 `internal/service/register.go`

注册 Service 不再忽略以下错误：

- 注册前查询邮箱失败；
- 创建用户失败。

错误会使用 `%w` 包装并继续返回给 Handler，不再出现“创建失败但接口返回注册成功、data 为 null”的情况。

### 3.3 `internal/handler/handler.go`

注册 Handler 现在区分：

- 邮箱已存在：HTTP 409；
- 其他注册错误：HTTP 500，响应 `注册失败`。

## 4. 房间创建与开播时间

### 4.1 `internal/model/room.go`

`StartAt` 从：

```go
time.Time
```

改为：

```go
*time.Time
```

房间尚未开播时，数据库写入 `NULL`，不再写入 MySQL 严格模式拒绝的：

```text
0000-00-00 00:00:00
```

### 4.2 `internal/model/room_detail.go`

详情 DTO 的 `StartAt` 同步改为 `*time.Time`，允许未开播房间返回 `null`。

### 4.3 `internal/repo/room.go`

开播时才创建真实时间并赋值：

```go
now := time.Now()
room.StartAt = &now
```

`CreateRoom` 遇到数据库错误时不再 `panic`，而是返回：

```go
fmt.Errorf("fatal error create room: %w", err)
```

### 4.4 `internal/handler/handler.go`

创建房间的非业务错误现在明确返回 HTTP 500 和 `房间创建失败`，不再出现空的 HTTP 200 响应。

## 5. WebSocket Hub 并发修复

### 5.1 `internal/ws/hub.go`

修复了以下问题：

1. `Hub.Run` 原来只循环 `workerNum` 次，处理少量注册/注销事件后就会退出；现在改为持续事件循环，直到 Context 取消。
2. 房间客户端集合原来部分使用 Hub 全局锁、部分无锁访问；现在按 Hub 房间字典锁和 Room 客户端集合锁分工访问。
3. 广播函数原来创建了客户端快照，但实际仍遍历原始 `room.clients`；现在真正使用快照。
4. 删除未实际用于发送的重复 JSON 序列化和 buffer pool 逻辑。
5. 广播序列化失败时记录错误并停止当前广播。
6. 在线人数系统消息中的 `onliene_count` 拼写统一为 `online_count`。

### 5.2 `internal/ws/client.go`

修复导致后端崩溃的竞态：

```text
panic: send on closed channel
```

主要修改：

- 增加 `sendMu` 和 `sendClosed`，统一保护发送队列的发送与关闭；
- 新增 `trySend`，所有队列写入都检查关闭状态；
- 新增 `closeSend`，保证发送队列只关闭一次；
- 新增 `signalUnregister`，通过 `sync.Once` 保证客户端只注销一次；
- 新增 `closeConnection`，通过 `sync.Once` 保证 WebSocket 连接只关闭一次；
- `readPump` 和 `writePump` 不再自行直接关闭 `send` channel；
- Hub 先把客户端从房间集合移除，再关闭该客户端发送队列；
- 正常的 WebSocket Close 1000 和 `websocket.ErrCloseSent` 不再当成异常日志输出。

## 6. WebSocket 回归测试

文件：`internal/ws/hub_test.go`

新增两个测试：

### 6.1 `TestHubRunProcessesMoreThanWorkerCount`

验证 Hub 能处理多于 worker 数量的客户端注册事件，防止事件循环再次被误写成有限循环。

### 6.2 `TestConcurrentBroadcastAndUnregister`

让广播与客户端注销并发发生，验证发送队列被安全关闭且不会再次发生 `send on closed channel`。

验证结果：

```text
go test ./...                         通过
go vet ./...                          通过
go test ./internal/ws -count=100      通过
```

Windows 本机未安装 GCC，因此无法运行 Go race detector。可在 Ubuntu 服务器执行：

```bash
go test -race ./internal/ws
```

## 7. 压测文档

文件：`docs/load-testing-guide.md`

文档记录了：

- Windows 到 Ubuntu 的 SSH 隧道流程；
- 注册、登录、创建房间和开播命令；
- WebSocket 小规模、端到端和逐级加压命令；
- IPv4、IPv6 直连方式；
- Kafka 独立吞吐测试；
- 服务端监控和测试清理；
- 自动脚本的参数与使用方式。

## 8. 数据库结构处理（非代码修改）

旧数据库表由历史模型创建，GORM `AutoMigrate` 无法安全替换已有主键，因此运行时曾出现自增 ID 为 0。

### 8.1 `users`

旧表的问题：

- `user_id` 可为 NULL；
- `user_id` 不是主键；
- `user_id` 没有 AUTO_INCREMENT。

保留备份后删除旧表，由当前模型重新创建。新结构为：

```text
user_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT
PRIMARY KEY (user_id)
```

### 8.2 `rooms` 与 `danmas`

旧 `rooms` 表同样不能生成有效 `room_id`。确认其中仅有测试数据后，删除 `danmas` 和 `rooms`，由当前模型重新创建：

```text
rooms.room_id  → PRIMARY KEY、AUTO_INCREMENT
rooms.start_at → 允许 NULL
danmas.id      → PRIMARY KEY、AUTO_INCREMENT
```

数据库删除与重建属于服务器环境操作，不是 Git 中的代码修改。

## 9. 首次完整自动测试结果

执行命令：

```powershell
PowerShell -ExecutionPolicy Bypass -File .\scripts\load-test.ps1
```

结果：

```text
连接数：             10
持续时间：           30 秒
发送弹幕：           143
收到广播：           1350
理论广播：           1430
benchmark errors：   0
历史消息匹配：       100（查询上限为 100）
最终结果：           AUTOMATED LOAD TEST PASSED
```

端到端链路已经验证：

```text
WebSocket
→ Kafka
→ MySQL
→ Redis Pub/Sub
→ WebSocket 广播
```

## 10. 已知限制与后续建议

1. benchmark 已增加 drain 阶段；如果 drain 后仍存在广播缺口，应继续检查 Kafka Consumer Lag、Redis Pub/Sub 和慢客户端，而不是把缺口简单归因于立即断开。
2. 历史接口单次最多返回 100 条，因此 `History matches: 100` 只能证明消息已经持久化，不能单独证明全部 143 条均已写入。
3. 正式性能测试应按以下顺序逐级增加连接：

```text
10 → 100 → 1,000 → 5,000 → 10,000 → 30,000
```

4. 上万连接前应同时监控 CPU、内存、文件描述符、Kafka Consumer Lag、Redis 和 MySQL。
5. `expected` 按消息发送时的当前连接数计算；若测试中途仍有连接异常退出，需结合 `peak_conns`、`dial_succeeded` 和服务端日志解释结果。

## 11. 高并发建连与统计口径修复

### 11.1 在线人数合并广播

文件：`internal/ws/hub.go`

旧实现每注册或注销一个连接，就立即向整个房间广播一次在线人数。集中建立 `N` 个连接时，客户端投递次数接近：

```text
1 + 2 + ... + N = N(N+1)/2
```

现在注册和注销只把对应房间标记为 dirty。独立 worker 每 250ms 最多读取一次该房间的最新人数，中间状态直接覆盖，不进入排队。弹幕仍使用原广播队列，不与“只需最新值”的在线人数共享事件语义。

具体代码变化：

- `Hub` 新增 `presenceDirty`、`presenceMu` 和 `presencePeriod`；
- `handleRegister`、`handleUnregister` 不再直接创建在线人数广播任务，只调用 `markPresenceDirty`；
- `presenceWorker` 每 250ms 读取一次所有 dirty 房间；
- `takeDirtyRooms` 取出房间后立即清空待处理标记，使同一周期内的多次变化自然合并；
- `broadcastOnlineCount` 重新读取房间当前连接数，只广播最新值；
- 在线人数直接由独立 worker 广播，不进入可能并发乱序的弹幕任务队列。

以 1,000 个连接、每 2ms 建立一个连接为例，旧实现的在线人数客户端投递量接近 `500,500`；按 250ms 合并后理论量约为 `4,500`，预计减少约 99%。实际数值会随 Dial 速度变化。

文件：`internal/ws/hub_test.go`

新增 `TestPresenceUpdatesAreCoalesced`，验证同一房间连续 100 次状态变化只产生一个在线人数帧。原有并发广播和注销测试继续保留。

### 11.2 游客与发送者分离

文件：`cmd/benchmark/main.go`

benchmark 先确定准确的活跃客户端集合。只有真正发送弹幕的客户端携带 `sid`；其余客户端不发送 Cookie，按游客连接，避免对每个监听连接执行 Redis Session 和用户数据库查询。

新增参数：

```text
-active-clients N
```

当该参数为 `-1` 时继续按 `-active` 比例换算；显式数量优先。

活跃客户端不再由每个 goroutine 随机决定，而是根据客户端编号均匀且精确地分配。例如 1,000 个连接配合 `-active-clients 10`，保证恰好 10 个客户端携带 `sid` 并发送弹幕，其余 990 个作为游客监听。这会把 Session Redis 查询和用户查询从约 1,000 次降到约 10 次；房间状态查询仍会按连接执行。

文件：`cmd/benchmark/main_test.go`

新增以下测试：

- `TestResolveActiveClientCount`：验证比例、最小一个发送者、纯监听和显式覆盖；
- `TestActiveClientsAreDistributedExactly`：验证 1,000 个客户端中准确选出 10 个发送者。

### 11.3 Dial、warm-up 与 drain

benchmark 现在会：

1. 等待所有连接尝试完成；
2. 记录 `dial_attempted`、`dial_succeeded`、`dial_failed` 和 `peak_conns`；
3. 按实际失败顺序输出前 10 个 Dial 错误；
4. warm-up 完成后统一启动发送；
5. 到达发送时长后停止发送，但在 drain 期间保持连接并继续接收广播；
6. drain 结束后关闭客户端并输出最终统计。

测试持续时间 `-d` 现在只计算实际发送阶段，不再从程序启动或爬升建连时开始计算。这样慢速 ramp 不会提前耗尽测试时间。

新增参数：

```text
-warmup 2s
-drain 5s
```

### 11.4 自动脚本与直连测试

文件：`scripts/load-test.ps1`、`docs/load-testing-guide.md`

PowerShell 脚本新增 `-ActiveClients`、`-Warmup` 和 `-Drain`，并要求 Dial 成功数等于目标连接数。测试指南补充了 IPv4/IPv6 直连命令；SSH 隧道保留用于小规模功能验证，不再作为 1,000 个以上连接的正式性能结论依据。

脚本通过条件同步加强为：

- `dial_attempted` 等于目标连接数；
- `dial_succeeded` 等于目标连接数；
- `dial_failed` 等于 0；
- `peak_conns` 等于目标连接数；
- benchmark 总 `errors` 等于 0；
- 发送模式下 `sent`、`received` 均大于 0；
- 历史接口能够找到 `bench-*` 消息。

IPv6 地址在 HTTP/WebSocket URL 中仍使用方括号；脚本执行 TCP 连通性检查时改用 `Uri.DnsSafeHost`，自动去除方括号后再传给 `TcpClient`。

### 11.5 本轮修改文件清单

| 文件 | 修改内容 |
|---|---|
| `internal/ws/hub.go` | 在线人数单槽最新状态、250ms 合并广播 |
| `internal/ws/hub_test.go` | 在线人数合并测试 |
| `cmd/benchmark/main.go` | 精确发送者、游客连接、Dial 指标、warm-up、drain |
| `cmd/benchmark/main_test.go` | 活跃客户端数量和分配测试 |
| `scripts/load-test.ps1` | 新参数、连接目标校验、IPv6 端口检查 |
| `docs/load-testing-guide.md` | 新参数说明、IPv4/IPv6 直连命令和 SSH 使用边界 |
| `docs/automated-load-test-change-log.md` | 本节修改记录 |

### 11.6 本地验证结果

已执行并通过：

```text
go test ./...
go vet ./...
go test ./internal/ws -count=100
PowerShell AST 语法检查
git diff --check
```

Windows 本机未启用 CGO，因此不能在本机执行 Go race detector。代码同步到 Ubuntu 服务器后还应执行：

```bash
go test -race ./internal/ws
go build -o live-stream .
```

服务器必须重新构建并启动新二进制后，本轮并发修改才会在远程压测中生效。
