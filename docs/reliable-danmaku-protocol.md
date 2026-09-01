# 弹幕实时与恢复协议

## 1. 服务端链路

```text
                              ┌→ realtime consumer group
WebSocket → Kafka RequireAll ─┤       → Redis Pub/Sub → WebSocket Gateway
                              └→ persistence consumer group
                                      → MySQL 批量写入
```

Kafka 是实时链路和持久化链路共同的可靠交接点：

- Producer 使用 `room_id` 作为 Key，同一房间固定进入一个 Kafka 分区；
- Producer 使用同步 `RequireAll`，成功后才向发送者返回 `accepted`；
- realtime consumer 在 Redis 发布成功后提交自己的 offset；
- persistence consumer 在 MySQL 事务成功后提交另一个 Consumer Group 的 offset；
- Redis 发布成功但 offset 提交前崩溃可能导致重复广播，客户端按 `message_id` 去重；
- MySQL 不再位于观众看到弹幕之前，数据库抖动不会阻塞实时广播。

旧的 MySQL Transactional Outbox 已退出实时路径。Kafka 本身已经是持久化日志，再让实时广播等待 MySQL 和 Outbox 会重复承担可靠交接职责，并把数据库延迟带入直播链路。

## 2. 发送弹幕

客户端生成 UUID，重试时必须复用相同 `message_id`：

```json
{
  "type": "danma",
  "message_id": "91a13b9a-a67e-49f5-98d4-3e747c42d8d1",
  "content": "hello"
}
```

Kafka 完成同步确认后，发送端收到：

```json
{
  "type": "accepted",
  "message_id": "91a13b9a-a67e-49f5-98d4-3e747c42d8d1"
}
```

`accepted` 只表示 Kafka 已持久确认，不表示所有观看者已经收到。未收到时可以使用相同 `message_id` 重试。

## 3. `room_seq` 是递增游标

两个 Consumer Group 都使用 Kafka 分区 offset 生成相同的 `room_seq`：

```text
room_seq = partition_offset + 1
```

同一房间固定在一个分区，所以 `room_seq` 对该房间严格递增。多个房间可能共享一个分区，因此它允许不连续：

```text
房间 14：151 → 154 → 158
```

客户端必须按数值排序，但不能把 `last_seq + 1` 缺失直接判定为丢消息。它是恢复游标，不是连续计数器。

Topic 分区数投入使用后不能直接增加；Kafka Hash 在分区数变化后可能把同一房间映射到新分区，破坏原有顺序。需要扩分区时应创建新 Topic，并同时引入 sequence epoch 或执行明确切换。

```json
{
  "type": "danma",
  "message_id": "91a13b9a-a67e-49f5-98d4-3e747c42d8d1",
  "room_id": 14,
  "room_seq": 158,
  "content": "hello"
}
```

## 4. 热点房间分片扇出

一个房间的连接按连接 ID 固定分成 16 个 shard：

```text
room 14
├─ shard 0 → fixed worker
├─ shard 1 → fixed worker
├─ ...
└─ shard 15 → fixed worker
```

每条消息只序列化并创建一次 `PreparedMessage`，随后把同一个只读帧指针投递给 16 个分片。同一 `room_id + shard` 始终映射到固定 FIFO worker，所以单个客户端不会被不同 worker 竞争写队列；不同 shard 可以并行利用多核。

## 5. 慢客户端与重同步

每个客户端有：

- 256 槽实时弹幕队列；
- 8 槽高优先级控制队列；
- 1 槽可覆盖状态队列，在线人数和点赞总数合并为 `room_state`，只保留最新值；
- 10 秒 WebSocket WriteDeadline；
- Ping/Pong 存活检测。

一次实时队列满不会立即断开：

1. 丢弃本次实时投递并增加 `ws_queue_full_total`；
2. 通过控制队列发送 `resync_required`，同一连接最多每 5 秒提示一次；
3. 连续满 10 次或持续满 1 秒，才用关闭码 4001 断开；
4. 客户端收到控制消息或重连后，使用最近游标补拉。

```json
{
  "type": "resync_required",
  "room_id": 14,
  "reason": "live_queue_gap"
}
```

## 6. ACK 的边界

发送者的 `accepted` 是必要确认。观看者 ACK 当前只记录连接内最大游标和协议负载指标，不维护服务端重传窗口，因此普通吞吐压测应关闭：

```powershell
-AckEvery 0
```

需要测 ACK 成本时再单独设置 `-AckEvery 50`。由于 `room_seq` 允许不连续，ACK 表示客户端已处理的最大游标，不表示中间每一个整数都对应本房间消息。

## 7. 重连补拉

以下情况调用恢复接口：

- 收到 `resync_required`；
- WebSocket 以 4001 关闭；
- 网络断开后重新连接；
- 客户端启动时恢复上次保存的游标。

```http
GET /api/v1/rooms/14/messages/replay?after_seq=150&limit=100
```

响应按 `room_seq ASC` 返回。若 `has_more=true`，继续使用 `next_seq` 请求。补拉与实时流可能交错到达，客户端按 `message_id` 去重、按 `room_seq` 排序。

## 8. 验证

先测纯扇出：

```powershell
.\scripts\load-test.ps1 `
  -Connections 1000 `
  -ActiveClients 10 `
  -AckEvery 0 `
  -MaxLossPercent 0 `
  -Duration 1m `
  -Drain 15s
```

再单独测 ACK：

```powershell
.\scripts\load-test.ps1 `
  -Connections 1000 `
  -ActiveClients 10 `
  -AckEvery 50 `
  -MaxLossPercent 0 `
  -Duration 1m `
  -Drain 15s
```

两个 Consumer Group 都应最终回到 `LAG=0`：

```bash
docker exec kafka kafka-consumer-groups --bootstrap-server localhost:29092 \
  --describe --group live-danmu-consumer

docker exec kafka kafka-consumer-groups --bootstrap-server localhost:29092 \
  --describe --group live-danmu-consumer-realtime
```

诊断指标：

```bash
curl http://127.0.0.1:6060/debug/ws/metrics
curl http://127.0.0.1:6060/debug/kafka/realtime
```

重点关注：

- `ws_broadcast_duration`：序列化并把任务分派给所有 shard 的耗时；
- `ws_fanout_shard_duration`：单个 shard 复制客户端快照并入队的耗时；
- `ws_client_queue_depth`：所有实时和控制队列当前积压；
- `ws_queue_full_total`：被丢弃并要求补拉的实时投递；
- `ws_resync_required_total`：发出的重同步提示；
- `ws_slow_client_disconnect_total`：持续过载后主动断开的客户端；
- `ws_write_errors_total`：Socket 写超时和写错误。
