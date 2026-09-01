# 弹幕可靠投递协议

## 1. 服务端链路

```text
WebSocket → Kafka(RequireAll，同步确认)
          → MySQL 事务写入 danmas + danma_outboxes
          → 提交 Kafka offset
          → Outbox Dispatcher 重试发布 Redis
          → WebSocket 广播
```

Kafka 重复消费通过 `message_id` 唯一索引去重。Outbox 发布采用至少一次语义：如果 Redis 发布成功后进程在标记 `published_at` 前退出，消息可能再次发布，客户端必须用 `message_id` 去重。

## 2. 发送弹幕

客户端应生成 UUID，并在重试时复用同一个 `message_id`：

```json
{
  "type": "danma",
  "message_id": "91a13b9a-a67e-49f5-98d4-3e747c42d8d1",
  "content": "hello"
}
```

Kafka 完成同步确认后，发送端会收到：

```json
{
  "type": "accepted",
  "message_id": "91a13b9a-a67e-49f5-98d4-3e747c42d8d1"
}
```

`accepted` 表示 Kafka 已确认，不表示所有观看者已经收到。没有收到 `accepted` 时，客户端可以使用相同 `message_id` 重试。

## 3. 接收、排序与累计 ACK

持久化后的弹幕包含房间内递增的 `room_seq`：

```json
{
  "type": "danma",
  "message_id": "91a13b9a-a67e-49f5-98d4-3e747c42d8d1",
  "room_id": 14,
  "room_seq": 151,
  "content": "hello"
}
```

客户端维护：

- 已连续处理的最大 `last_seq`；
- 已展示的 `message_id` 集合，用于去重；
- 暂存乱序消息的小缓冲区。

每处理 50 条或每隔 1 秒发送一次累计 ACK：

```json
{
  "type": "ack",
  "room_id": 14,
  "ack_seq": 151
}
```

ACK 不进入 Kafka。服务端当前把 ACK 保存在连接内并计入 `ws_ack_total`，用于确认客户端进度和观测协议负载。

## 4. 缺失和重连补拉

出现以下情况时调用恢复接口：

- 收到的 `room_seq > last_seq + 1`；
- WebSocket 断线或收到关闭码 `4001 resync_required`；
- 定期校准时需要确认是否漏掉最后一条消息。

```http
GET /api/v1/rooms/14/messages/replay?after_seq=150&limit=100
```

响应按 `room_seq` 升序返回：

```json
{
  "data": {
    "items": [],
    "next_seq": 150,
    "has_more": false
  },
  "message": "恢复历史弹幕成功"
}
```

如果 `has_more=true`，继续使用响应中的 `next_seq` 请求，直到为 false。补拉和实时消息可能交错到达，客户端必须按 `room_seq` 排序并按 `message_id` 去重。

## 5. 数据库迁移

启动时会：

1. 为 `danmas` 增加 `room_seq`；
2. 按每个房间原有 `id` 顺序回填旧数据；
3. 建立唯一索引 `(room_id, room_seq)`；
4. 创建 `room_message_sequences` 和 `danma_outboxes`；
5. 用历史最大序号初始化房间序号表。

首次在正式数据库运行前仍应做数据库快照，并先在测试库执行迁移。不要同时启动两个正在执行首次迁移的新版本实例。

## 6. 验证命令

启用累计 ACK 的端到端测试：

```powershell
.\scripts\load-test.ps1 `
  -Connections 1000 `
  -ActiveClients 10 `
  -AckEvery 50 `
  -MaxLossPercent 0 `
  -Duration 1m `
  -Drain 15s
```

测试后检查：

```sql
SELECT room_id, MIN(room_seq), MAX(room_seq), COUNT(*)
FROM danmas
GROUP BY room_id;

SELECT COUNT(*) AS pending_outbox
FROM danma_outboxes
WHERE published_at IS NULL;
```

正常稳定状态下 `pending_outbox` 应回落到 0。
