# 弹幕实时与恢复协议

## 1. 服务端链路

```text
                              ┌→ realtime consumer group
WebSocket → Kafka RequireAll ─┤       ├→ 弹幕：Redis Pub/Sub → WebSocket Gateway
                              │       └→ 点赞：MySQL 聚合 → Redis 状态 → Gateway
                              └→ persistence consumer group
                                      → 弹幕历史 MySQL 批量写入
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

1. 丢弃队列中最旧的实时弹幕，保留最新弹幕，增加 `ws_dropped_oldest_total`；
2. 通过控制队列发送带精确游标的 `resync_required`，同一连接最多每 5 秒提示一次；
3. 热门房间达到 75% 高水位时提前执行“丢旧保新”，避免排队到满才处理；
4. 默认连续积压 100 次或持续 5 秒，才用关闭码 4001 断开；
5. 客户端收到控制消息或重连后，使用最近游标补拉。

```json
{
  "type": "resync_required",
  "room_id": 14,
  "reason": "live_queue_gap",
  "after_seq": 151,
  "latest_seq": 158,
  "replay_url": "/api/v1/rooms/14/messages/replay"
}
```

`after_seq` 是该连接最后成功写入 Socket 的游标，`latest_seq` 是触发缺口的最新事件游标。客户端从 `after_seq` 开始补拉，直到达到 `latest_seq`；补拉和实时流中的重复事件按 `message_id` 去重。

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

恢复接口优先读取 Redis 最近消息 ZSet；只有 `after_seq` 早于缓存窗口或 Redis 查询失败时才回源 MySQL。响应中的 `source` 为 `redis` 或 `mysql`：

```json
{
  "data": {
    "items": [],
    "next_seq": 158,
    "has_more": false,
    "source": "redis"
  }
}
```

实时消费者使用一个 Lua 脚本原子完成 `ZADD + 按数量裁剪 + EXPIRE + PUBLISH`，保证客户端收到恢复提示时，对应弹幕已经进入最近消息缓存。默认每房间保留 1000 条、TTL 300 秒。

## 8. 热门房间自适应

Hub 按 1 秒窗口估算：

```text
预计扇出/秒 = 源弹幕/秒 × 当前本机房间连接数
```

- 在线人数达到 5000 时标记为热门房间；
- 预计扇出不超过 150000 次/秒时仍完整推送；
- 超过预算后计算 `sample_stride=ceil(预计扇出/预算)`，最高为 16；
- 被采样掉的消息仍完整保存在 Kafka、Redis 最近缓存和 MySQL，只减少实时 WebSocket 扇出；
- 模式变化通过高优先级控制队列发送 `delivery_policy`；
- 热门房间在线人数更新由 250 ms 合并周期放宽为 1000 ms。

定位纯 WebSocket 推送极限时，可以暂时把 `adaptive_sampling_enabled` 设为 `false`；业务稳定性测试应保持开启，否则超过预算后仍会继续把压力灌入慢客户端队列。

当前 benchmark 也识别 `delivery_policy`：进入 sampled 后按 `poll_interval_ms` 补拉，恢复 push 时再追赶一次，并把成功补回的唯一消息计入 `recovered`。这允许自动化测试分别验证“纯推送极限”和“过载降级后最终完整交付”。

```json
{
  "type": "system",
  "room_id": 14,
  "data": {
    "sub_type": "delivery_policy",
    "delivery_mode": "sampled",
    "sample_stride": 3,
    "poll_interval_ms": 1000
  }
}
```

浏览器参考实现位于 `web/danmaku-client.js`。它在 sampled 模式下使用随机抖动的 HTTP 补拉，恢复 push 模式后停止轮询；WebSocket 断开时指数退避重连，重连成功后从本地保存的游标补拉，并按 `message_id` 去重。

```javascript
import { DanmakuClient } from "/danmaku-client.js";

const client = new DanmakuClient({
  wsUrl: "ws://127.0.0.1:9000/api/v1/ws",
  apiBase: "http://127.0.0.1:9000",
  roomId: 14,
  onDanmaku: (message) => renderDanmaku(message),
  onMode: (mode) => console.log("delivery mode:", mode),
});
client.start();
```

跨域浏览器调用使用与 WebSocket 相同的 `server.allowed_origins` 白名单，并允许凭证 Cookie。正式环境应使用 HTTPS/WSS；当前登录 Cookie 带 `Secure`，普通 HTTP 只适合匿名观看或命令行压测。

## 9. 点赞聚合与故障边界

点赞仍采用“不同 `message_id` 表示不同点击”的连续点赞语义，但不再为每次点击写一行 MySQL。入口先在 Redis 原子完成 `message_id` 预留和单用户/单房间限流；Kafka 同步确认后，预留标记转为已接收状态。默认去重窗口为 24 小时，预留租约为 60 秒。

realtime consumer 按 100 条或 200 ms 刷新。一次刷新按 Kafka Topic、Partition 和房间聚合，MySQL 事务内完成：

1. 锁定 `(consumer_group, topic, partition)` 聚合 checkpoint；
2. 丢弃 `offset <= last_offset` 的重放事件；
3. 每个房间写一条 `like_batches`，记录 `batch_id`、offset 范围和 `delta`；
4. 每个房间只执行一次 `rooms.like_count = like_count + delta`；
5. 在同一事务中推进 checkpoint。

`batch_id` 由 consumer group、Topic、Partition、room_id 和本批 offset 范围确定。checkpoint 额外解决了一个关键问题：进程重启后批次边界可能变化，不能只依靠“旧批次 ID 再插一次会冲突”来去重。

事务后的严格顺序为：

```text
MySQL 聚合事务成功
→ Redis 写入绝对 like_count 并确认 message_id 标记
→ 发布最新 room_state
→ 提交 Kafka offset
```

因此，MySQL 成功但 Kafka offset 尚未提交时崩溃，只会造成重放，不会重复计数；MySQL 失败时不会越过尚未落库的点赞提交更高 offset。MySQL 是累计点赞数真相源，Redis 是实时镜像，Kafka 在保留期内保存原始点击事件，`like_batches` 保存聚合审计记录。

注意：单次 `message_id` 的客户端重试去重是有 TTL 的 Redis 约束，不是永久 MySQL 明细约束；Kafka 重放幂等则由 MySQL checkpoint 持久保证。这是用固定空间和更少热点写换取吞吐的明确取舍。

相关配置：

```yaml
danmaku:
  like:
    max_per_second: 10
    batch_size: 100
    batch_wait_milliseconds: 200
    idempotency_ttl_seconds: 86400
    reservation_ttl_seconds: 60
```

## 10. 验证

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
- `ws_dropped_oldest_total`：慢连接队列中被替换的旧弹幕；
- `ws_sampled_messages_total`：热门房间自适应策略跳过的实时广播；
- `ws_hot_rooms`：当前被判定为热门的本机房间数；
- `ws_hot_room_policies`：热门房间的连接数、估算源 QPS、估算扇出 QPS 和采样步长；
- `ws_resync_required_total`：发出的重同步提示；
- `ws_slow_client_disconnect_total`：持续过载后主动断开的客户端；
- `ws_write_errors_total`：Socket 写超时和写错误。
- `kafka_realtime_like_batch_total`：已写入 MySQL 的聚合批次数；
- `kafka_realtime_like_event_total`：已计入聚合的原始点赞数；
- `kafka_realtime_like_batch_persist_duration`：点赞聚合事务耗时分布。
