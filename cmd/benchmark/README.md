# 压测工具

## WebSocket

连接压测默认创建 30,000 个连接。目标房间必须存在且处于开播状态，`-origin` 必须包含在后端 `server.allowed_origins` 中。

仅测试连接和广播接收：

```powershell
go run ./cmd/benchmark -c 60000 -r 1s -room 1 -origin http://localhost:5173
```

同时测试弹幕发送时，需要先登录并把 Cookie 中的 `sid` 传入。建议只设置一个发送连接，其余连接作为观众接收广播：

```powershell
go run ./cmd/benchmark -c 60000 -r 1s -room 1 -origin http://localhost:5173 -sid "<SID>" -active-clients 1
```

常用参数：

- `-urls`：一个或多个逗号分隔的 WebSocket 地址，默认 `ws://localhost:9000/api/v1/ws`。
- `-c`：连接数，默认 30,000。
- `-room`：房间 ID，默认 1。
- `-r`：活跃客户端的平均发送间隔，默认 1 秒。
- `-active`：发送消息的客户端比例，默认 0.001；未提供 `-sid` 时自动降为 0。
- `-active-clients`：精确指定发送连接数；项目按用户和房间限制弹幕频率，复用同一个 `sid` 时不要用它模拟多个独立用户。
- `-ramp`：建立相邻连接的间隔，默认 1 毫秒。
- `-d`：持续时间，默认 0，表示运行到 Ctrl+C。

每条压测弹幕都会带上发送时间。最终结果按服务端返回的 `accepted` 计算理论投递数：`expected = accepted × 测试开始时的稳定连接数`，被限流或 Kafka 写入失败的 `rejected` 不再错误计入理论投递数。`realtime_received` 是 WebSocket 实时收到的唯一消息数，`recovered` 是补拉恢复数，`received` 是两者去重后的最终送达数。

延迟也拆成实时、补拉和合并三组固定桶直方图。`realtime_latency_p95` 更适合判断实时体验，合并后的 `latency_p95` 会包含补拉耗时。统计过程不会保存逐条延迟样本。

ACK 协议成本测试可以增加 `-ack-every 50`。每个客户端每处理约 50 条带 `room_seq` 的弹幕后确认已看到的最大 Kafka 游标；该游标对单房间递增，但多个房间共享分区时允许不连续。默认值 `0` 不发送 ACK，也是纯扇出测试的推荐值。压测弹幕同时携带客户端生成的 UUID `message_id`，用于验证服务端幂等链路。最终输出中的 `resync_required` 统计服务端发出的重同步控制消息。

通过 SSH 隧道测试远端服务时，仍可以使用默认 `ws://localhost:9000`，例如：

```powershell
ssh -L 9000:127.0.0.1:9000 <SSH_USER>@<SERVER_HOST>
go run ./cmd/benchmark -c 30000 -room 1 -origin http://localhost:5173
```

单机 60,000 个连接可能接近本机临时端口、文件描述符和内存上限。应先从 1,000 个连接逐步增加。

## Kafka

Kafka 工具默认读取 `config/config.yaml` 的 broker 和 topic，写入 1,000,000 条消息：

```powershell
go run ./cmd/benchmark/kafka
```

指定消息量和并发数：

```powershell
go run ./cmd/benchmark/kafka -n 5000000 -w 300 -room 1
```

常用参数：

- `-brokers`：逗号分隔的 broker 地址；默认读取项目配置。
- `-topic`：目标 topic；默认读取项目配置。
- `-n`：消息总数，默认 1,000,000。
- `-w`：并发写入 goroutine 数，默认 100。
- `-async`：是否异步写入，默认 true。
- `-batch`：每批最大消息数，默认 1,000。
- `-batch-timeout`：批次最长等待时间，默认 10 毫秒。

如果使用正式 `danmu_topic`，在线消费者会把压测消息写入 MySQL 并发布到 Redis。只测试 Kafka broker 时，应使用独立压测 topic，或先停止业务消费者。
