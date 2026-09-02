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

## 15. 热点房间有序并行与 Kafka 双消费链路（2026-09-01）

本轮针对 10,000 连接、10 个发送者测试中出现的持续积压、6227 个连接断开和约 42% 最终投递缺口进行结构调整。不能把结果简单归因于单 worker，因此同时保留分派耗时、单 shard 扇出耗时和客户端队列水位指标，用后续 profile 判断真正瓶颈。

### 15.1 同房间客户端分片

- 一个房间的连接按连接 ID 固定分为 16 个 shard；
- 每个 `room_id + shard` 固定映射到一个 FIFO worker；
- 一条消息只 Marshal 和创建一次 PreparedMessage；
- 16 个 shard 可并行，同一客户端不会被多个 worker 竞争入队；
- 新增测试验证客户端均匀分片、映射稳定和每客户端 FIFO 顺序。

这与“不同弹幕随机交给任意 worker”不同。随机处理可以提高表面吞吐，但会让同一客户端乱序。

### 15.2 慢客户端宽限和控制队列

- 实时队列继续保持 256，不扩大为高内存的 4096；
- 增加容量 8 的控制队列，优先传递 `accepted`、错误和 `resync_required`；
- 在线人数与点赞总数合并成 `room_state`，使用独立容量 1 状态槽；新状态覆盖旧状态，不进入弹幕积压队列；
- 第一次实时队列满只丢本次投递并通知重同步；持续出现缺口时同一连接最多每 5 秒提示一次；
- 连续满 10 次或持续满 1 秒后才用 4001 关闭；
- WriteDeadline 调整为 10 秒，并增加 Ping/Pong 存活检测；
- 新增 `control_queue_full`、`resync_required` 和 `slow_client_disconnect` 指标。

严格零缺口测试仍会把任何丢弃判为失败。宽限的意义是避免一次瞬时抖动让数千连接同时退出，不是把丢失伪装成成功。

### 15.3 Kafka 取代 Outbox 热路径

旧路径：

```text
Kafka → MySQL + Outbox → Redis → WebSocket
```

新路径：

```text
                              ┌→ live-danmu-consumer-realtime → Redis → WebSocket
WebSocket → Kafka RequireAll ─┤
                              └→ live-danmu-consumer → MySQL
```

- realtime group 在 Redis Publish 成功后提交 offset；
- 点赞事件由 realtime group 在 Redis 中累加，并按刷新周期只广播最新总数，避免每次点赞都扇出到全房间；
- 点赞最新状态也必须发布成功后才提交 realtime offset；Kafka offset 提交成功不再逐批打印，避免 200ms 批次在压测期间形成日志 I/O 热点；
- persistence group 在 MySQL 成功后提交 offset；
- 两条链路互不等待；
- `message_id` 处理至少一次广播造成的重复；
- `room_id` Hash Key 保证同一房间固定 Kafka 分区；
- `room_seq=partition_offset+1`，对单房间严格递增但允许不连续；
- 删除 Outbox Dispatcher、Outbox Repo 和 Outbox Model，旧数据库中的遗留表不会再被运行时代码使用。

### 15.4 ACK 与压测口径

- 普通扇出压测保持 `AckEvery=0`；
- ACK 仅用于单独测协议成本；
- benchmark 不再假设 `room_seq` 必须连续，而是确认已处理的最大 Kafka 游标；
- 最终输出增加 `resync_required`，用于区分主动重同步和普通 Socket 错误；
- 当前 benchmark 仍未自动执行 4001 后重连补拉，因此恢复闭环需要单独测试。

### 15.5 新增诊断指标

```text
ws_fanout_shard_duration
ws_control_queue_full_total
ws_resync_required_total
ws_slow_client_disconnect_total
```

`ws_broadcast_duration` 现在表示单条消息编码并分派到 16 个 shard 队列的耗时；`ws_fanout_shard_duration` 表示一个 shard 复制客户端快照并入队的耗时。

### 15.6 验证

本地已经执行：

```text
go test ./...
go vet ./...
git diff --check
```

部署后必须先跑 100、1000 连接且 `AckEvery=0`，再逐级增加到 10,000。服务端同时保存 `/debug/ws/metrics`、两个 Consumer Group 的 Lag、CPU profile、网卡吞吐和压测机资源；不能仅凭连接数判断优化成功。

### 15.7 低流量实时延迟优化

100 个连接、1 个发送者的隔离测试仍出现 `P50<=2s`、`P95<=5s`，证明该延迟不是热点房间扇出负载造成的。本轮进一步调整：

- realtime Kafka Reader 的 `MaxWait` 从 1 秒降为 50ms，`ReadBatchTimeout` 从 5 秒降为 1 秒；
- realtime Reader 设置 `CommitInterval=200ms`，Redis 发布仍然先完成，但 `CommitMessages` 只将 offset 交给 kafka-go 的异步提交循环，不再阻塞 Fetch/Publish 主循环；
- persistence Consumer 保留原有批量参数，不牺牲 MySQL 吞吐；
- 新增 `/debug/kafka/realtime`，输出 Kafka 消息年龄、Redis Publish 耗时和 commit 入队耗时；
- `/debug/ws/metrics` 新增 `ws_realtime_message_age`，用于判断消息抵达 Redis Subscriber 时已经经历的时间；
- benchmark 延迟桶增加 750ms、1.5s、3s 和 4s，缩小高延迟区间的估算误差；
- PowerShell 脚本新增 `-MaxP95Latency`，例如 `-MaxP95Latency 500ms`；超过阈值时即使零丢失也判定失败。

异步提交可能在进程异常退出时重复处理最近一个提交周期内的消息，但不会造成已发布消息丢失；客户端继续按 `message_id` 去重。

### 15.8 修正 Producer 固定批次等待

部署 15.7 后，100 连接、1 个发送者仍然稳定表现为 `P50<=2s`、`P95<=5s`，说明只调整 Consumer 的判断不成立。继续检查 kafka-go 源码后确认：业务 Producer 未配置 `BatchTimeout`，默认值为 1 秒、默认 `BatchSize` 为 100。低流量下一条消息不足以填满 batch，每次同步 `WriteMessages` 会等待定时刷新，这是端到端延迟的主要固定来源。

本轮修正：

- Producer 显式设置 `BatchTimeout=5ms`、`BatchSize=100`；
- 保留 `Async=false` 和 `RequireAll`，发送者仍在 Kafka 确认后收到 `accepted`；
- 低流量最多等待约 5ms，高流量仍可以在 5ms 窗口内合批；
- `/debug/kafka/realtime` 增加 `kafka_producer_write_duration`；
- 增加单元测试，防止以后删除配置后悄悄退回 kafka-go 的 1 秒默认值。

15.7 的 Consumer 低等待和异步 offset 提交继续保留，但它们不是本次秒级延迟的主要修复。

## 14. PreparedMessage 与可靠投递闭环

### 14.1 PreparedMessage 生产接入

根据后续改造要求，WebSocket Client 的发送队列已从 `chan []byte` 改为 `chan *websocket.PreparedMessage`。同一条房间广播只进行一次 JSON 编码和一次 PreparedMessage 构造，所有目标连接共享该只读帧，并通过 `WritePreparedMessage` 写出。

当前短基准中 PreparedMessage 的耗时约为 207.6 μs/op，普通 WriteMessage 约为 212.6 μs/op，但 PreparedMessage 分配量仍更高。历史长基准曾出现 PreparedMessage 略慢的结果，因此该优化最终是否保留，需要以 Linux 服务端 10,000 连接全链路 profile 和压测结果决定，不能仅凭微基准下结论。

### 14.2 房间序号和恢复接口

- `danmas` 增加房间内递增的 `room_seq`；
- 启动迁移按原 `id` 顺序回填旧数据；
- 新增 `(room_id, room_seq)` 唯一索引；
- 新增 `GET /api/v1/rooms/:room_id/messages/replay?after_seq=N&limit=100`；
- 恢复接口按 `room_seq ASC` 返回，供断线和缺口补拉。

### 14.3 ACK 和发送幂等

- 客户端可以发送 `{type:"ack", room_id, ack_seq}` 累计 ACK；
- ACK 不进入 Kafka，连接内只允许确认位置向前推进；
- 服务端指标增加 `ws_ack_total`；
- 压测器增加 `-ack-every`，可以模拟累计 ACK 负载；
- 发送端可提供 UUID `message_id`，超时重试时复用；
- Kafka 同步确认后返回 `accepted`；无效 UUID 返回 400 类型的 WebSocket 错误消息。

### 14.4 Transactional Outbox

Kafka Consumer 不再直接执行“写 MySQL 后立即 Redis Publish”。现在同一个 MySQL 事务完成：

```text
分配 room_seq → 插入 danmas → 插入 danma_outboxes
```

事务成功后提交 Kafka offset。独立 Outbox Dispatcher 按数据库顺序发布 Redis，失败时指数退避重试，成功后设置 `published_at`。发布成功但标记前崩溃可能造成重复，因此客户端必须按 `message_id` 去重。

Kafka Producer 同时从异步改为 `Async=false + RequireAll`，使发送端的 `accepted` 建立在 Kafka 确认之后。

详细客户端协议见 `docs/reliable-danmaku-protocol.md`。

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

## 12. Kafka Consumer 定时刷新和诊断改进

文件：`internal/kafka/consumer.go`

旧实现在 `select` 的 `default` 分支中直接调用阻塞的 `FetchMessage`。当 Kafka 暂时没有新消息时，主循环无法响应 200ms ticker，尾部不满 100 条的批次会长时间滞留。

现在改为：

- 独立 goroutine 阻塞调用 `FetchMessage`，并把结果交给主循环；
- 主循环可以正常响应 200ms ticker，定时处理未满批次；
- 优雅退出前使用最多 10 秒的独立 Context 刷新剩余消息；
- `MinBytes` 从 10KB 改为 1，避免低流量弹幕因等待凑满请求大小而增加延迟；
- 明确配置 `MaxWait=1s` 和 `ReadBatchTimeout=5s`；
- 接入 kafka-go 的 `ErrorLogger`，使连接 Broker、寻找 Coordinator 和加入 Consumer Group 的内部错误能在后端日志中直接看到。
- 提交批次时把所有 Kafka 消息传给 `CommitMessages`，由 kafka-go 分别提交每个分区的最高 offset，不再只提交批次最后一条所在的分区。

消费组 `live-danmu-consumer` 不需要手动创建；新二进制成功连接 Kafka 并加入后，Kafka 会自动创建并显示该组。

文件：`internal/kafka/producer.go`、`main.go`

Producer 初始化不再只创建异步 Writer 就打印成功。启动时会在 10 秒超时内实际连接 `danmu_topic` 的 partition 0 leader；Broker、advertised listener 或 topic 不可达时返回明确错误。`main.go` 现在检查该错误并停止启动，避免出现 HTTP 服务正常、Kafka 实际不可用的假健康状态。

## 13. WebSocket 压测可信度、写路径和可观测性优化

本轮修改先完善压测判定和可观测性，再调整慢客户端治理与广播调度。多 Gateway、消息 ACK、历史补拉和 Outbox 不在本轮单机优化范围内。

### 13.1 严格校验投递损失和重复投递

文件：`scripts/load-test.ps1`

新增参数：

```powershell
[ValidateRange(0.0, 100.0)]
[double]$MaxLossPercent = 0.0
```

脚本现在会从 benchmark 的 `[FINAL]` 行同时解析 `received` 和 `expected`，计算：

```text
missing = max(0, expected - received)
lossPercent = missing * 100 / expected
```

发送模式下新增两项严格检查：

- `received > expected` 时按重复投递失败；
- `lossPercent > MaxLossPercent` 时按投递损失超限失败。

`MaxLossPercent` 默认值为 0，即默认要求零投递缺口。可按需要放宽，例如：

```powershell
# 严格零缺口
.\scripts\load-test.ps1 -MaxLossPercent 0

# 最多允许 0.1% 缺失
.\scripts\load-test.ps1 -MaxLossPercent 0.1
```

最终摘要新增 `Expected deliveries`、`Missing deliveries` 和 `Loss`，避免只检查“至少收到一条消息”造成误判。

### 13.2 端到端延迟固定桶统计

文件：`cmd/benchmark/main.go`、`cmd/benchmark/main_test.go`

压测弹幕内容改为：

```text
bench-{clientID}-{sequence}-{unixNano}
```

接收端从最后一个字段解析发送时间，计算从客户端发送到同一压测进程收到 WebSocket 广播的端到端延迟。延迟使用固定桶和原子计数器统计，不保存逐条样本，因此内存不会随投递次数线性增长。

周期输出和最终输出新增：

```text
latency_samples
latency_p50
latency_p95
latency_p99
```

新增测试覆盖时间戳格式校验和固定桶分位数计算。

### 13.3 区分发送队列关闭与队列满

文件：`internal/ws/client.go`、`internal/ws/hub.go`

`trySend` 不再返回无法区分原因的布尔值，改为明确状态：

```text
enqueueOK
enqueueClosed
enqueueFull
```

处理语义如下：

- `enqueueOK`：记录成功入队；
- `enqueueClosed`：连接已经进入注销流程，不记录队列满；
- `enqueueFull`：增加队列满计数，立即关闭 Socket，并且只发送一次注销信号。

队列满时先关闭 Socket，可以打断可能阻塞在系统调用中的写协程。逐客户端的 `channel full` 和写错误日志已经移除，避免大量连接同时退出时形成日志风暴。

### 13.4 WriteDeadline 和关闭状态

文件：`internal/ws/client.go`

每次 WebSocket 写入前设置 5 秒 deadline：

```go
const writeWait = 5 * time.Second
```

`closeConnection` 会先用原子状态标记连接正在关闭，再关闭底层 Socket。`readPump` 和 `writePump` 的退出清理也调整为先标记并关闭连接，再发送注销信号。

只有非主动关闭状态下的 `SetWriteDeadline` 或 `WriteMessage` 错误才会计入 `ws_write_errors_total`。这样 readPump 主动关闭连接后产生的预期写失败不会被当作真实运行错误。

### 13.5 按房间固定广播 Worker

文件：`internal/ws/hub.go`、`internal/ws/redis_subscriber.go`

原来的单个 `broadcastTasks` channel 由多个 worker 共同消费，同一房间连续消息可能被不同 worker 并发处理。现在改为按房间分片的队列：

```text
queueIndex = roomID % workerCount
```

每个队列只由一个 worker 消费，Redis Subscriber 统一通过 `enqueueBroadcast` 路由消息。效果如下：

- 同一房间始终进入同一个队列，保持处理顺序；
- 不同房间可以由不同 worker 并行广播；
- 单个超大房间仍然受单 worker 扇出速度限制，不等同于解决单房间的无限扩展问题。

在线人数是可覆盖的状态更新，继续由独立 presence worker 直接处理，不占用弹幕事件队列。

### 13.6 WebSocket 指标和本机 pprof

文件：`internal/ws/metrics.go`、`main.go`

新增指标：

```text
ws_connections
ws_broadcast_messages_total
ws_delivery_enqueued_total
ws_queue_full_total
ws_write_errors_total
ws_closed_clients_total
ws_broadcast_duration
ws_client_queue_depth
```

`ws_broadcast_duration` 使用固定耗时桶；`ws_client_queue_depth` 在获取快照时汇总当前房间内所有客户端的待发送队列深度。服务每 10 秒输出一次连接数、队列满、写错误、关闭客户端数和队列深度汇总。

诊断服务只监听本机：

```text
127.0.0.1:6060
```

使用方法：

```bash
curl http://127.0.0.1:6060/debug/ws/metrics
go tool pprof "http://127.0.0.1:6060/debug/pprof/profile?seconds=60"
```

应用 HTTP 服务同步改为显式 `http.Server`，收到退出信号后会在 10 秒超时内关闭业务 HTTP 服务和诊断服务，不再依赖无法主动关闭的 `gin.Engine.Run`。

### 13.7 PreparedMessage 基准结论

文件：`internal/ws/prepared_message_benchmark_test.go`

增加了真实 loopback WebSocket 基准，模拟约 1KB 消息向 32 个连接扇出。基准严格按生产语义执行：每条广播创建一次 PreparedMessage，仅在该条消息对应的连接之间复用。

当前未启用 WebSocket 压缩的配置下，稳定轮次结果约为：

```text
WriteMessage          411～415 μs/op   约 70 KB/op   160 allocs/op
WritePreparedMessage  421～435 μs/op   约 84 KB/op   175 allocs/op
```

PreparedMessage 在本机结果中约慢 2%～6%，同时增加约 19% 的分配字节数和 15 次分配。因此本轮没有把 PreparedMessage 留在生产写路径，继续使用“一次 JSON Marshal、所有客户端共享同一字节切片”的实现。基准文件保留，后续启用压缩、升级依赖或改变消息尺寸后可以重新评估。

复现命令：

```bash
go test ./internal/ws -run '^$' -bench '^BenchmarkWebSocketFanoutFrame$' -benchtime=2s -count=3
```

### 13.8 测试覆盖与本地验证

文件：`internal/ws/hub_test.go`、`cmd/benchmark/main_test.go`

新增或扩展测试覆盖：

- 队列正常、关闭、满三种返回状态；
- 队列满时连接立即进入关闭状态，并且只发送注销信号；
- 广播只序列化一次并在客户端之间共享 payload；
- 同一房间稳定映射到同一广播队列；
- 指标 HTTP 接口和广播耗时计数；
- benchmark 时间戳解析和 P50/P95/P99 固定桶统计。

已执行并通过：

```text
go test ./...
go vet ./...
go test -count=20 ./internal/ws ./cmd/benchmark
PowerShell AST 语法检查
git diff --check
```

本机缺少 GCC，Go race detector 依赖的 CGO 无法启用，因此仍需在具备 C 编译器的 Linux 环境执行：

```bash
go test -race ./internal/ws ./cmd/benchmark
```

本轮没有启动完整的 MySQL、Redis、Kafka 和后端环境，因此尚未执行新的全链路压力测试。部署新二进制后应先执行严格零缺口测试，再采集 CPU profile：

```powershell
.\scripts\load-test.ps1 -Connections 1000 -ActiveClients 10 -MaxLossPercent 0
```

## 16. 慢客户端、热门房间和推拉降级

本轮新增以下能力：

- 实时队列由“本次消息直接丢弃”改为“丢最旧、保最新”；热门房间达到队列高水位即提前替换旧消息；
- `resync_required` 携带连接最后成功写出的 `after_seq`、触发缺口的 `latest_seq` 和补拉地址；
- 慢连接默认连续积压 100 次或持续 5 秒才断开，相关阈值进入 `config.yaml`；
- Redis 最近消息缓存使用 ZSet，实时 Consumer 用 Lua 原子完成缓存、裁剪、TTL 和 Pub/Sub；
- replay 接口在缓存能够覆盖游标时直接读取 Redis，超出窗口再回源 MySQL；
- Hub 以在线人数和预计 fan-out QPS 判定热门房间，超过预算时启用可配置采样；
- 新连接会立即收到房间当前 `delivery_policy`，模式切换走高优先级控制队列；
- benchmark 收到 `resync_required` 后自动调用 HTTP replay；收到 sampled 策略后按服务端间隔轮询，恢复 push 时完成最后一次追赶，并按 `message_id` 去重计数；
- 新增 `web/danmaku-client.js`，实现浏览器端断线重连、补拉、去重、sampled 模式轮询；
- HTTP API 增加与 WebSocket 相同 Origin 白名单的凭证化 CORS 支持。

新增指标：

```text
ws_hot_rooms
ws_sampled_messages_total
ws_dropped_oldest_total
```

注意：采样是过载降级，不是零丢失推送。完整消息仍位于 Kafka、Redis 最近缓存和 MySQL；客户端是否把补拉消息重新展示到弹幕轨道，应根据消息时效决定。

### 13.9 本轮修改文件清单

| 文件 | 修改内容 |
|---|---|
| `scripts/load-test.ps1` | 严格损失阈值、重复投递校验、最终损失摘要 |
| `cmd/benchmark/main.go` | 发送时间戳、固定桶延迟 P50/P95/P99 |
| `cmd/benchmark/main_test.go` | 时间戳和延迟直方图测试 |
| `cmd/benchmark/README.md` | 延迟指标说明 |
| `internal/ws/client.go` | 明确入队状态、关闭标记、WriteDeadline、聚合写错误 |
| `internal/ws/hub.go` | 慢客户端治理、按房间分片广播、指标采集 |
| `internal/ws/redis_subscriber.go` | 统一调用按房间路由入口 |
| `internal/ws/metrics.go` | WebSocket 指标快照、HTTP 接口和周期汇总日志 |
| `internal/ws/hub_test.go` | 队列状态、慢客户端、分片和指标测试 |
| `internal/ws/prepared_message_benchmark_test.go` | PreparedMessage 对比基准 |
| `main.go` | 本机 pprof/指标服务及 HTTP 优雅退出 |
| `docs/load-testing-guide.md` | 严格损失、延迟指标、服务指标和 pprof 使用说明 |
| `docs/automated-load-test-change-log.md` | 本节修改记录 |

## 17. 点赞持久幂等、榜单一致性与 Redis 恢复

本轮保留“同一用户可以连续点赞”的直播产品语义，但将“不同 message_id 是不同点赞、同一 message_id 只能计数一次”落实为持久化约束。

### 17.1 持久幂等

新增 `like_events` 表，`message_id` 为主键。实时点赞消费者在同一个 MySQL 事务中：

1. 幂等插入点赞事件；
2. 仅首次插入时执行 `rooms.like_count = like_count + 1`；
3. 返回当前权威点赞数。

如果消费者在数据库成功后、Redis 更新或 Kafka offset 提交前退出，重新消费相同消息时事件插入是成功的空操作，不会再次增加计数。

### 17.2 Redis 镜像与恢复

MySQL 的 `rooms.like_count` 现在是权威数据，Redis `room:like:<room_id>` 是实时镜像：

- 每次幂等事务成功后立即更新 Redis；
- 服务启动时恢复所有房间点赞数；
- 首次升级会保留比 MySQL 更大的旧 Redis 计数，避免直接丢弃历史点赞；
- 之后按配置周期从 MySQL 修复缺失或落后的 Redis Key；
- 新 WebSocket 房间从 MySQL 房间记录初始化 like_count，因此 Redis 刚丢失时也不会向新观众显示 0。

恢复 Lua 不会用旧 MySQL 快照覆盖更大的 Redis 值，避免与并发点赞产生回退竞态。

### 17.3 频率限制

新增单用户、单房间固定窗口限流，默认每秒最多 10 次。超过阈值返回 WebSocket error 429。配置：

```yaml
danmaku:
  like:
    max_per_second: 10
    reconcile_interval_seconds: 30
```

### 17.4 自动化回归

`scripts/full-test.ps1` 与 `cmd/e2e` 现在检查：

- 相同点赞 message_id 重放后计数不增加；
- rooms.like_count 能反映 WebSocket 已观察到的点赞；
- 源码存在持久幂等事务和 Redis 恢复路径；
- 超过配置频率的点赞返回 429；
- MySQL 中相同弹幕 message_id 仍只保存一条。

本轮涉及文件：

| 文件 | 修改内容 |
|---|---|
| `internal/model/like_event.go` | 点赞幂等事件模型 |
| `internal/dao/db.go` | 自动迁移 like_events |
| `internal/repo/like.go` | 事件去重与房间计数原子事务 |
| `internal/kafka/realtime_consumer.go` | 点赞持久化、Redis 镜像、提交顺序 |
| `internal/dao/redis.go` | 点赞限流及 MySQL→Redis 恢复 |
| `internal/ws/client.go` | 点赞频率限制 |
| `internal/ws/handler.go`、`hub.go` | 新连接初始化持久化点赞数 |
| `main.go` | 启动恢复与周期对账 |
| `scripts/full-test.ps1`、`cmd/e2e/main.go` | 点赞幂等与限流回归 |
