# 可靠性与压测演进记录

本文面向项目维护者，记录自动化测试、实时链路和高并发治理中的主要设计变更。运行行为以当前源码和 `config/config.yaml` 为准；具体执行命令见 [负载测试指南](load-testing-guide.md) 和 [全套自动化测试指南](testing/full-automated-test-guide.md)。

## 1. 自动化测试入口

项目提供两个功能对等的测试入口：

- `scripts/full-test.ps1`：Windows PowerShell；
- `scripts/full-test.sh`：Linux 原生 Bash。

测试流程覆盖：

```text
go test / go vet
→ 后端连通性检查
→ 注册、登录与 Session 验证
→ 创建房间并开播
→ WebSocket 协议验证
→ Kafka 实时广播与 MySQL 持久化验证
→ 历史分页和断线补拉验证
→ 点赞幂等、限流与状态一致性验证
→ 可选的分级连接压测
→ 自动关播并生成报告
```

测试账号和房间名称均带随机后缀，避免重复执行时发生唯一键冲突。脚本结束时会尝试停止测试房间，但项目没有删除用户和房间的接口，因此测试数据仍会保留。

### 1.1 测试档位

| 档位 | 范围 |
|---|---|
| `Functional` | 功能和协议检查，不执行连接压测 |
| `Smoke` | 功能检查及 100 个连接 |
| `Standard` | 100 和 1,000 个连接 |
| `Stress` | 1,000 和 5,000 个连接 |
| `Extreme` | 增加 10,000 个连接，仅用于隔离压测环境 |

### 1.2 通过条件

压测阶段同时校验以下指标：

- 建连尝试数、成功数和峰值连接数均达到目标；
- `dial_failed=0` 且运行错误数为 0；
- 发送模式下 `sent`、`received` 均大于 0；
- 接收数不超过理论投递数，避免把重复投递误判为成功；
- 投递损失不超过 `MaxLossPercent`；
- 配置延迟阈值时，P95 不超过 `MaxP95Latency`；
- 历史接口能够查询到本次测试产生的 `bench-*` 消息。

测试报告写入 `test-results/`，同时提供适合程序读取的 JSON 和适合人工检查的 Markdown。

## 2. API 与数据模型修正

### 2.1 注册链路

`SelectUserByEmail` 将 `gorm.ErrRecordNotFound` 视为正常的“邮箱尚未注册”，返回 `(nil, nil)`。查询失败和创建用户失败会继续向上返回，Handler 按错误类型响应：

- 邮箱已存在：HTTP 409；
- 数据库或内部错误：HTTP 500。

该约束避免创建失败时仍返回成功响应。

### 2.2 房间时间字段

`Room.StartAt` 和房间详情 DTO 使用 `*time.Time`。未开播房间写入 `NULL`，开播时才设置实际时间，避免 MySQL 严格模式拒绝零时间。

### 2.3 旧数据库结构

早期数据表可能缺少正确的主键、自增属性或可空约束。`AutoMigrate` 不负责安全替换已有主键，升级旧环境前应先备份并核对：

```text
users.user_id  → PRIMARY KEY、AUTO_INCREMENT
rooms.room_id  → PRIMARY KEY、AUTO_INCREMENT
rooms.start_at → 允许 NULL
danmas.id      → PRIMARY KEY、AUTO_INCREMENT
```

结构修复属于部署操作，不应由应用在未知数据状态下自动删表。生产或共享环境必须使用经过审查的迁移脚本。

## 3. WebSocket 并发与连接生命周期

### 3.1 Hub 事件循环和锁边界

Hub 持续处理注册、注销和广播事件，直到 Context 取消。锁的职责分为两层：

- Hub 锁保护全局房间字典；
- Room 锁保护房间内客户端集合和状态。

广播先复制客户端快照，再在快照上投递，避免遍历过程中与注销流程竞争。

### 3.2 安全注销

客户端连接使用独立状态和 `sync.Once` 控制注销与底层连接关闭。发送队列的写入和关闭由统一入口管理，避免并发路径触发 `send on closed channel`。正常关闭不计为运行错误。

### 3.3 在线人数合并

连接集中建立时，若每次注册都立即向全房间广播在线人数，投递量会接近 `N(N+1)/2`。当前实现只将房间标记为待更新，由独立协程按周期读取最新人数并合并广播。弹幕仍进入有序事件队列，不与可覆盖的状态更新共用语义。

## 4. 压测统计口径

### 4.1 发送者与观众分离

`-active-clients` 指定准确的发送者数量。只有发送者携带 Session，其余连接作为匿名观众监听，避免把不必要的鉴权和数据库查询计入纯扇出成本。

### 4.2 建连、预热与排空

压测器将测试过程分为：

1. 按 `ramp` 建立全部连接；
2. 等待 `warmup`，让在线状态和写队列稳定；
3. 在 `duration` 内发送消息；
4. 停止发送，在 `drain` 内继续接收尾部广播；
5. 输出最终统计并关闭连接。

`duration` 只计算发送阶段，不包含建连爬升时间。

### 4.3 延迟和损失

压测消息携带纳秒时间戳。接收端使用固定桶统计 `latency_p50`、`latency_p95` 和 `latency_p99`，不会保存逐条样本。最终结果同时输出：

```text
sent
received
expected
missing
loss
latency_samples
latency_p50
latency_p95
latency_p99
```

SSH 隧道适合功能验证和小规模测试。评估大规模连接时应优先直连，避免把加密、通道复用和本地端口限制误认为服务端瓶颈。

## 5. Kafka 实时与持久化链路

### 5.1 Producer 确认语义

Producer 使用同步 `RequireAll`。只有 Kafka 确认写入后，发送者才会收到 `accepted`。低流量场景显式设置较短的 `BatchTimeout`，避免沿用 kafka-go 默认批次等待造成接近一秒的固定延迟。

Producer 初始化会实际检查 Broker、Topic 和分区 Leader；Kafka 不可用时应用启动失败，避免 HTTP 已监听但消息链路不可用的假健康状态。

### 5.2 双 Consumer Group

实时展示与历史持久化使用两个独立消费者组：

```text
                              ┌→ realtime group → Redis → WebSocket
WebSocket → Kafka RequireAll ─┤
                              └→ persistence group → MySQL
```

- realtime group 负责 Redis 最近消息缓存、Pub/Sub 和点赞聚合；
- persistence group 负责弹幕历史批量写入 MySQL；
- 两条链路分别提交 offset，MySQL 延迟不会阻塞实时广播。

实时路径不再使用 Transactional Outbox。Kafka 已承担持久化日志和可靠交接职责，额外等待 MySQL Outbox 会重复职责并增加展示延迟。

### 5.3 房间游标

Producer 以 `room_id` 作为 Kafka Key，同一房间稳定进入同一分区：

```text
room_seq = partition_offset + 1
```

`room_seq` 在单个房间内递增，但多个房间共享分区时允许出现间隔。客户端将其作为排序和恢复游标，不假设相邻值必然加一。

## 6. 有序并行扇出与慢客户端治理

### 6.1 房间内分片

房间连接按连接 ID 固定映射到 16 个 shard。每个 `room_id + shard` 始终由同一个 FIFO worker 消费：

- 不同 shard 可以并行扇出；
- 同一客户端不会由多个 worker 竞争入队；
- 单条消息只编码一次，并复用只读的 `PreparedMessage`。

### 6.2 队列职责

每个客户端使用三类队列：

- 实时弹幕队列：有界，过载时丢旧保新；
- 控制队列：优先传递 `accepted`、`error` 和 `resync_required`；
- 状态槽：保存最新的在线人数和点赞数，旧状态可覆盖。

实时队列首次出现缺口时发送 `resync_required`，持续高水位达到阈值后才关闭连接，避免瞬时抖动造成大规模断连。

### 6.3 热门房间降级

Hub 根据在线连接数和预计扇出 QPS 判断房间负载。超过预算时切换为 sampled 模式：

- WebSocket 仅推送部分实时消息；
- 完整消息仍保存在 Kafka、Redis 最近消息缓存和 MySQL；
- 客户端按 `poll_interval_ms` 调用 replay 接口补拉；
- 恢复 push 模式时再执行一次追赶。

采样是过载保护，不等于实时流零丢失。完整性交付依赖补拉和 `message_id` 去重。

## 7. 点赞一致性与批量持久化

点赞入口在 Redis 中原子完成频率限制和 `message_id` 预留。Kafka 确认后，预留标记转为已接受状态；写入失败时释放预留。

realtime consumer 按条数或等待时间刷新，并按 Topic、Partition 和房间聚合。MySQL 事务内完成：

1. 锁定分区 checkpoint；
2. 跳过不大于 `last_offset` 的重放事件；
3. 为每个房间写入一条 `like_batches(delta)`；
4. 执行一次 `rooms.like_count += delta`；
5. 推进同一事务中的 checkpoint。

事务成功后依次更新 Redis 绝对计数、确认幂等标记、广播 `room_state`，最后提交 Kafka offset。

早期逐点击 `like_events` 方案已被聚合模型替代。当前边界为：

- Kafka 消费重放由 MySQL checkpoint 永久吸收；
- 客户端重复 `message_id` 由 Redis TTL 保证；
- 原始点赞事件在 Kafka 保留期内可追溯；
- MySQL 保存聚合审计记录，不保存无界增长的逐点击明细。

## 8. 实时热度榜

`rooms.like_count` 是历史累计点赞的权威展示值，不直接参与实时排序。Redis `rooms:hot` 使用以下短期信号计算热度：

- 跨网关在线连接数；
- 近一分钟点赞贡献；
- 近一分钟弹幕数；
- 开播时长衰减。

分钟桶用于近似滑动窗口；同一用户的热度点赞贡献受到独立频率限制。Redis 排名不可用时，列表按 `start_at DESC, room_id DESC` 降级，不回退到累计点赞排序。

热度权重属于可配置的工程初值，不代表线上实验结论。`hot_score` 允许秒级延迟和少量误差，`like_count` 的持久一致性不受影响。

## 9. 可观测性

诊断服务默认只监听 `127.0.0.1:6060`，提供：

```text
/debug/ws/metrics
/debug/kafka/realtime
/debug/pprof/
```

重点指标包括：

| 指标 | 含义 |
|---|---|
| `ws_connections` | 当前 WebSocket 连接数 |
| `ws_broadcast_duration` | 单条消息编码和分派耗时 |
| `ws_fanout_shard_duration` | 单个 shard 的快照与入队耗时 |
| `ws_client_queue_depth` | 客户端队列总深度 |
| `ws_dropped_oldest_total` | 丢旧保新次数 |
| `ws_resync_required_total` | 重同步提示次数 |
| `ws_slow_client_disconnect_total` | 持续过载断连次数 |
| `ws_sampled_messages_total` | sampled 模式跳过的实时投递数 |
| `kafka_producer_write_duration` | Producer 同步确认耗时 |
| `kafka_realtime_message_age` | realtime consumer 读取时的消息年龄 |
| `kafka_realtime_like_batch_total` | 点赞聚合批次数 |
| `kafka_realtime_like_batch_persist_duration` | 点赞聚合事务耗时 |

诊断端口不应直接暴露到公网。远程分析可使用 SSH 端口转发。

## 10. 跨平台支持

- Linux 使用 `scripts/full-test.sh`，无需安装 PowerShell；
- Windows 使用 `scripts/full-test.ps1`；
- 前端开发服务器通过 `--hostname 0.0.0.0` 支持局域网访问；
- 前端使用同源代理连接后端，避免 Session Cookie 和 WebSocket Origin 跨站问题；
- 服务端渲染与浏览器首帧使用稳定快照，避免 Hydration 不一致。

## 11. 验证命令

代码级验证：

```bash
go test ./...
go vet ./...
go test -race ./internal/ws ./cmd/benchmark
```

前端验证：

```bash
cd frontend
pnpm lint
pnpm build
```

WebSocket 扇出基准：

```bash
go test ./internal/ws -run '^$' \
  -bench '^BenchmarkWebSocketFanoutFrame$' \
  -benchtime=2s -count=3
```

完整链路还需在 MySQL、Redis、Kafka 和后端均已启动的隔离环境中运行自动化脚本。性能结果必须同时记录服务端和压测机的 CPU、内存、网络、文件描述符、Kafka Consumer Lag 及诊断指标。

## 12. 运维边界

- `Stress` 和 `Extreme` 仅用于专用测试环境；
- Kafka 独立吞吐测试应使用专用 Topic，避免业务消费者写入大量测试数据；
- 公网直连压测时，安全组和防火墙只允许压测机来源地址；
- 故障注入不得在共享环境中自动停止 Redis、Kafka 或 MySQL；
- 旧数据库升级前必须备份并审查迁移步骤；
- Topic 分区数变化会影响 `room_id` 的分区映射，扩容应通过新 Topic 和明确的切换方案完成。
