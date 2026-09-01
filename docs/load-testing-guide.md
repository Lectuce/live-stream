# Live Stream 压测操作手册

本文档描述如何使用 Windows 作为压测机，对部署在 Ubuntu 服务器上的 `live-stream` 执行完整压测，包括：

- SSH 端口转发和连通性验证；
- 注册、登录、创建房间和开播；
- WebSocket 连接压测；
- WebSocket → Kafka → realtime Consumer → Redis → WebSocket 实时压测，并行验证 persistence Consumer → MySQL；
- IPv4、IPv6 直连压测；
- Kafka 独立吞吐压测；
- 服务器监控和测试结束清理。

## 1. 测试拓扑

小规模功能验证推荐使用 SSH 隧道：

```text
Windows 压测程序
→ 127.0.0.1:19000
→ SSH 隧道
→ Ubuntu 服务器 127.0.0.1:9000
→ live-stream
```

正式高并发测试推荐直接连接服务器：

```text
Windows 压测程序
→ 服务器 IPv4/IPv6:9000
→ live-stream
```

SSH 隧道适合功能验证和中小规模测试。它会引入额外加密和通道复用开销，不适合准确衡量 30,000～60,000 个连接时的真实网络性能。

## 2. 命令中的占位符

执行命令前替换以下内容：

| 占位符 | 含义 | 示例 |
|---|---|---|
| `SERVER_IPV4` | 服务器公网 IPv4 | `203.0.113.10` |
| `SERVER_IPV6` | 服务器公网 IPv6 | `2001:db8::10` |
| `SSH_USER` | SSH 用户名 | `yujun` |
| `ROOM_ID` | 实际测试房间 ID | `1` |
| `SID_VALUE` | 登录响应中的 Session ID | 一段十六进制字符串 |

用户名和 `@` 之间不能添加反斜杠。正确格式是：

```text
yujun@SERVER_IPV4
```

不是：

```text
yujun\@SERVER_IPV4
```

## 3. 压测前的代码检查

先确认当前版本能够编译：

```bash
cd ~/projects/live-stream
go test ./...
go build -o live-stream .
```

`[no test files]` 表示包通过编译，但仓库没有对应的单元测试；它不代表 MySQL、Redis、Kafka 和业务接口已通过运行时验证。

执行端到端测试前，还要确认以下数据库字段名与当前模型一致：

```text
user_email
user_id
user_name
start_at
```

如果运行日志出现以下错误：

```text
Unknown column 'user_emal'
Unknown column 'u.username'
Unknown column 'u.id'
Unknown column 'room.started_at'
```

应检查：

```text
internal/repo/user.go
internal/repo/room.go
```

当前模型对应的 SQL 字段应为：

```text
user_emal      → user_email
u.username     → u.user_name
u.id           → u.user_id
room.started_at → room.start_at
```

创建新用户或新房间时，查询不到记录是正常情况。`SelectUserByEmail` 和 `SelectRoomByOwnerID` 应将 `gorm.ErrRecordNotFound` 转换成 `(nil, nil)`，而不是作为服务器错误返回。

端到端弹幕测试还要求 Kafka Consumer 的批量插入重试逻辑正确。预期规则是：

```text
插入成功 → 立即结束重试并继续发布 Redis、提交 offset
插入失败 → 等待后重试
最终仍失败 → 不发布 Redis、不提交 offset
```

## 4. 配置 WebSocket Origin

服务器的 `config/config.yaml` 至少加入：

```yaml
server:
  allowed_origins:
    - "http://localhost:5173"
```

压测程序会主动发送这个 Origin。修改配置后必须重启后端。

## 5. 建立 SSH 隧道并启动服务

### 5.1 Windows 第一个 PowerShell

通过 IPv4 建立隧道并打开服务器 Shell：

```powershell
ssh.exe -4 -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:19000:127.0.0.1:9000 `
  SSH_USER@SERVER_IPV4
```

也可以通过 IPv6 建立 SSH 连接：

```powershell
ssh.exe -6 -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:19000:127.0.0.1:9000 `
  SSH_USER@SERVER_IPV6
```

输入密码并进入服务器：

```text
SSH_USER@VM:~$
```

保持这个 PowerShell 窗口打开。窗口一旦关闭，本地 19000 端口会立即停止监听。

如果只希望建立隧道而不打开服务器 Shell，可以使用 `-N`：

```powershell
ssh.exe -4 -N -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:19000:127.0.0.1:9000 `
  SSH_USER@SERVER_IPV4
```

输入密码后窗口没有输出是正常现象。

### 5.2 在服务器启动后端

在已经登录的服务器 Shell 中执行：

```bash
cd ~/projects/live-stream
go build -o live-stream .
./live-stream
```

看到以下日志表示主要组件已经启动：

```text
Connect success!
kafka writer init success
danmu consumer started
redis subscriber started
Listening and serving HTTP on :9000
```

如果服务已经由 systemd、Docker 或另一个 Shell 启动，不要重复运行。可以检查：

```bash
ss -lntp | grep ':9000'
curl -i http://127.0.0.1:9000/api/v1/rooms
```

## 6. 验证 Windows 隧道

打开 Windows 第二个 PowerShell。不要关闭第一个 SSH 窗口。

检查本地端口：

```powershell
Test-NetConnection 127.0.0.1 -Port 19000
```

预期结果：

```text
TcpTestSucceeded : True
```

检查监听进程：

```powershell
netstat -ano | findstr ":19000"
```

预期看到 `LISTENING`。

定义后端地址并验证 HTTP：

```powershell
$Base = "http://127.0.0.1:19000"
curl.exe -i "$Base/api/v1/rooms"
```

如果本地 19000 不监听，使用详细 SSH 日志重新连接：

```powershell
ssh.exe -4 -vvv -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:19000:127.0.0.1:9000 `
  SSH_USER@SERVER_IPV4
```

重点检查：

```text
Local forwarding listening
forwarding failed
administratively prohibited
bind
Permission denied
```

## 7. 创建测试账号和 Session

以下命令在 Windows 第二个 PowerShell 中执行。

生成不会重复的账号：

```powershell
$Stamp = Get-Date -Format "MMddHHmmss"
$UserName = "bench$Stamp"
$Email = "bench$Stamp@example.com"
$Password = "Benchmark123!"
```

注册：

```powershell
$RegisterBody = @{
    username = $UserName
    email    = $Email
    password = $Password
} | ConvertTo-Json -Compress

$RegisterResult = Invoke-RestMethod `
  -Method Post `
  -Uri "$Base/api/v1/auth/register" `
  -ContentType "application/json" `
  -Body $RegisterBody

$RegisterResult | ConvertTo-Json -Depth 5
```

登录：

```powershell
$LoginBody = @{
    email    = $Email
    password = $Password
} | ConvertTo-Json -Compress

$LoginResponse = Invoke-WebRequest `
  -UseBasicParsing `
  -Method Post `
  -Uri "$Base/api/v1/auth/login" `
  -ContentType "application/json" `
  -Body $LoginBody
```

提取 Session ID：

```powershell
$SetCookie = $LoginResponse.Headers["Set-Cookie"] -join ";"
$Sid = [regex]::Match($SetCookie, "sid=([^;]+)").Groups[1].Value

if ([string]::IsNullOrWhiteSpace($Sid)) {
    throw "登录响应中没有 sid"
}

$Sid
```

验证 Session：

```powershell
Invoke-RestMethod `
  -Method Get `
  -Uri "$Base/api/v1/auth/me" `
  -Headers @{ Cookie = "sid=$Sid" }
```

测试脚本会显式发送 Cookie Header，因此即使测试环境使用 HTTP，也可以使用取得的 `sid`。浏览器本地联调时则需要根据 HTTP/HTTPS 环境正确设置 Cookie 的 `Secure` 属性。

## 8. 创建并启动测试房间

创建房间：

```powershell
$CreateRoomBody = @{
    title = "Benchmark Room"
} | ConvertTo-Json -Compress

$CreateRoomResult = Invoke-RestMethod `
  -Method Post `
  -Uri "$Base/api/v1/rooms" `
  -ContentType "application/json" `
  -Headers @{ Cookie = "sid=$Sid" } `
  -Body $CreateRoomBody

$CreateRoomResult | ConvertTo-Json -Depth 5
```

取得并验证房间 ID：

```powershell
$RoomId = [uint64]$CreateRoomResult.data.room_id

if ($RoomId -le 0) {
    throw "没有取得有效 room_id"
}

$RoomId
```

开播：

```powershell
$StartBody = @{
    room_title = "Benchmark Room"
} | ConvertTo-Json -Compress

Invoke-RestMethod `
  -Method Post `
  -Uri "$Base/api/v1/rooms/$RoomId/start" `
  -ContentType "application/json" `
  -Headers @{ Cookie = "sid=$Sid" } `
  -Body $StartBody
```

预期返回：

```text
开播成功
```

WebSocket Handler 会拒绝不存在或未开播的房间，因此必须完成这一步。

## 9. WebSocket 小规模功能测试

在 Windows 第二个 PowerShell 进入本地项目：

```powershell
Set-Location D:\GoProjects\live-stream
```

如果换了新的 PowerShell 窗口，需要重新设置：

```powershell
$RoomId = ROOM_ID
$Sid = "SID_VALUE"
```

### 9.1 单连接验证

```powershell
go run .\cmd\benchmark `
  -urls "ws://127.0.0.1:19000/api/v1/ws" `
  -origin "http://localhost:5173" `
  -room $RoomId `
  -c 1 `
  -active-clients 0 `
  -warmup 2s `
  -drain 5s `
  -d 15s
```

成功指标：

```text
conns=1
errors=0
```

### 9.2 十连接验证

```powershell
go run .\cmd\benchmark `
  -urls "ws://127.0.0.1:19000/api/v1/ws" `
  -origin "http://localhost:5173" `
  -room $RoomId `
  -c 10 `
  -active-clients 0 `
  -warmup 2s `
  -drain 5s `
  -d 30s
```

这一阶段只验证连接建立和保持，不发送弹幕，因此 `sent` 和 `received` 可以为 0。

## 10. 端到端弹幕测试

让 10 个连接全部发送弹幕：

```powershell
go run .\cmd\benchmark `
  -urls "ws://127.0.0.1:19000/api/v1/ws" `
  -origin "http://localhost:5173" `
  -room $RoomId `
  -sid $Sid `
  -c 10 `
  -active-clients 10 `
  -r 2s `
  -warmup 2s `
  -drain 5s `
  -d 1m
```

这会测试：

```text
WebSocket
→ Kafka
→ MySQL
→ Redis Pub/Sub
→ WebSocket 广播
```

成功指标：

```text
conns=10
sent/s > 0
recv/s > 0
errors=0
```

查询历史消息：

```powershell
$History = Invoke-RestMethod `
  -Method Get `
  -Uri "$Base/api/v1/rooms/$RoomId/messages?limit=50"

$History | ConvertTo-Json -Depth 10
```

历史消息中应包含 `bench-...` 内容。

如果 `sent/s > 0` 但 `recv/s = 0`，依次检查：

1. Kafka Producer 是否报错；
2. Kafka Consumer 是否读取消息；
3. `processBath` 成功/失败重试条件是否写反；
4. MySQL 是否写入弹幕；
5. Redis 是否向 `live:danmu:broadcast` 发布；
6. Redis Subscriber 是否仍在运行。

## 11. SSH 隧道下逐级增加连接

先测试 1,000 个连接：

```powershell
go run .\cmd\benchmark `
  -urls "ws://127.0.0.1:19000/api/v1/ws" `
  -origin "http://localhost:5173" `
  -room $RoomId `
  -sid $Sid `
  -c 1000 `
  -active-clients 10 `
  -r 1s `
  -ramp 2ms `
  -warmup 3s `
  -drain 5s `
  -d 2m
```

推荐按以下顺序增加：

```text
10 → 1,000 → 5,000 → 10,000
```

每一级都要确认：

```text
connected 接近目标值
errors 没有持续增长
服务器 CPU 和内存可接受
Kafka、Redis、MySQL 没有明显堆积
```

## 12. IPv4 直连正式压测

正式压测前，在云安全组和服务器防火墙中，仅允许 Windows 压测机的公网 IP 访问 TCP 9000。不要长期向 `0.0.0.0/0` 开放。

Windows 验证：

```powershell
Test-NetConnection SERVER_IPV4 -Port 9000
```

1,000 连接直连验证：

```powershell
go run .\cmd\benchmark `
  -urls "ws://SERVER_IPV4:9000/api/v1/ws" `
  -origin "http://localhost:5173" `
  -room $RoomId `
  -sid $Sid `
  -c 1000 `
  -active-clients 10 `
  -r 1s `
  -ramp 2ms `
  -warmup 3s `
  -drain 5s `
  -d 2m
```

确认成功后再测试 10,000、30,000。

```powershell
go run .\cmd\benchmark `
  -urls "ws://SERVER_IPV4:9000/api/v1/ws" `
  -origin "http://localhost:5173" `
  -room $RoomId `
  -sid $Sid `
  -c 30000 `
  -active-clients 30 `
  -r 1s `
  -ramp 1ms `
  -warmup 5s `
  -drain 10s `
  -d 5m
```

## 13. IPv6 直连正式压测

先验证服务器 IPv6：

```powershell
ping -6 SERVER_IPV6
Test-NetConnection SERVER_IPV6 -Port 9000
```

WebSocket URL 中的 IPv6 必须用方括号包围：

```powershell
go run .\cmd\benchmark `
  -urls "ws://[SERVER_IPV6]:9000/api/v1/ws" `
  -origin "http://localhost:5173" `
  -room $RoomId `
  -sid $Sid `
  -c 30000 `
  -active-clients 30 `
  -r 1s `
  -ramp 1ms `
  -warmup 5s `
  -drain 10s `
  -d 5m
```

也可以让压测连接在 IPv4 和 IPv6 之间轮询：

```powershell
go run .\cmd\benchmark `
  -urls "ws://SERVER_IPV4:9000/api/v1/ws,ws://[SERVER_IPV6]:9000/api/v1/ws" `
  -origin "http://localhost:5173" `
  -room $RoomId `
  -sid $Sid `
  -c 60000 `
  -active-clients 60 `
  -r 1s `
  -ramp 1ms `
  -warmup 5s `
  -drain 10s `
  -d 5m
```

Windows 单机 60,000 个出站连接接近临时端口上限。更可靠的方式是使用两台以上压测机分摊连接。

## 14. Kafka 独立吞吐压测

Kafka 压测推荐直接在服务器项目目录执行，避免暴露 Kafka 到公网。

少量验证：

```bash
cd ~/projects/live-stream
go run ./cmd/benchmark/kafka -n 10000 -w 10 -room 1
```

正式测试：

```bash
go run ./cmd/benchmark/kafka -n 1000000 -w 100 -room 1
```

高压力测试：

```bash
go run ./cmd/benchmark/kafka -n 5000000 -w 300 -room 1
```

默认写入 `config/config.yaml` 中配置的业务 topic。如果两个业务 Consumer Group 仍在运行，消息会由实时组发布到 Redis，并由持久化组独立写入 MySQL。

只测试 Kafka broker 时，应使用独立 topic：

```bash
go run ./cmd/benchmark/kafka \
  -topic danmu_benchmark \
  -n 1000000 \
  -w 100 \
  -room 1
```

Kafka 输出指标：

```text
queued         已进入 Writer 队列的消息数
delivered      broker 已确认的消息数
failed         broker 写入失败的消息数
write_errors   WriteMessages 调用错误数
enqueue_qps    消息排队速度
delivered_qps  包含最终 flush 的实际完成速度
```

## 15. 服务器监控

压测期间打开额外的 SSH 窗口。

CPU、内存：

```bash
top
```

每秒查看 9000 端口连接数：

```bash
watch -n 1 "ss -tan | grep ':9000' | wc -l"
```

查看整体 Socket 状态：

```bash
ss -s
```

查看文件描述符限制：

```bash
ulimit -n
```

30,000～60,000 个连接建议后端进程的文件描述符上限至少为：

```text
100000
```

如果依赖服务运行在 Docker：

```bash
docker stats
```

重点观察：

- 后端 CPU、内存和 goroutine 数；
- TCP 已建立连接数；
- WebSocket `errors`、`lag`、`loss`；
- Kafka Consumer Lag；
- Redis CPU、内存和网络；
- MySQL 连接数、慢查询和写入延迟。

服务同时在 `127.0.0.1:6060` 提供仅本机可访问的诊断接口：

```bash
curl http://127.0.0.1:6060/debug/ws/metrics
curl http://127.0.0.1:6060/debug/kafka/realtime
go tool pprof "http://127.0.0.1:6060/debug/pprof/profile?seconds=60"
```

`/debug/ws/metrics` 包含连接数、广播数、成功入队数、队列满次数、写错误、关闭客户端数、广播耗时直方图和客户端队列总深度。服务每 10 秒还会汇总输出队列满、写错误和关闭客户端计数，不会为每个断开的客户端逐条打印日志。

## 16. 测试结束与清理

### 16.1 停止直播

Windows PowerShell：

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri "$Base/api/v1/rooms/$RoomId/stop" `
  -Headers @{ Cookie = "sid=$Sid" }
```

### 16.2 停止压测

指定 `-d` 时，压测会自动结束。手动停止使用：

```text
Ctrl+C
```

### 16.3 停止后端

在运行 `./live-stream` 的服务器窗口按 `Ctrl+C`。

### 16.4 关闭 SSH 隧道

后端退出并回到服务器 Shell 后：

```bash
exit
```

或者在隧道窗口按 `Ctrl+C`。

验证 Windows 本地端口已经关闭：

```powershell
Test-NetConnection 127.0.0.1 -Port 19000
```

此时预期：

```text
TcpTestSucceeded : False
```

### 16.5 删除临时安全组规则

如果为了 IPv4/IPv6 直连测试开放了服务器 TCP 9000，测试结束后立即删除对应的云安全组和服务器防火墙规则。

如果压测使用了业务 Kafka topic，测试数据会保存在 MySQL。需要保留业务数据时，不要直接执行批量删除；应通过明确的 `message_id` 前缀或专用测试房间进行定向清理。

## 17. 常见错误速查

| 现象 | 常见原因 |
|---|---|
| 本地 19000 不监听 | SSH 窗口已关闭、命令未成功认证、本地端口冲突 |
| WebSocket 403 | Origin 不在白名单、房间尚未开播 |
| WebSocket 404 | 房间不存在 |
| WebSocket 能发送但收不到 | realtime Kafka Consumer、Redis 发布或 Subscriber 异常；MySQL 不再阻塞实时广播 |
| HTTP 500 + Unknown column | Repo SQL 字段名与 GORM 模型不一致 |
| 大量 `connection refused` | 服务未监听、连接建立速度过快、端口或资源限制 |
| Windows 约 60,000 连接后失败 | 本机临时端口接近耗尽 |
| `too many open files` | 服务器文件描述符上限过低 |
| SSH 日志出现 `administratively prohibited` | SSH 服务禁止 TCP Forwarding |

## 18. 推荐执行顺序

每一级成功后再进入下一级：

```text
编译通过
→ 服务器本地 HTTP 成功
→ SSH 隧道成功
→ Windows HTTP 成功
→ 注册/登录成功
→ 创建/开播成功
→ 1 个 WebSocket 连接
→ 10 个连接
→ 10 个连接端到端弹幕
→ 1,000 个连接
→ 5,000 个连接
→ 10,000 个连接
→ IPv4/IPv6 直连
→ 30,000/60,000 个连接
→ Kafka 独立吞吐测试
→ 停止直播并清理测试资源
```

## 19. Windows 自动端到端测试

项目提供了 `scripts/load-test.ps1`，它会自动完成：

```text
代码编译和静态检查
→ 后端连通性检查
→ 注册和登录
→ 验证 Session
→ 创建房间并开播
→ WebSocket 端到端压测
→ 检查历史弹幕
→ 自动关播
```

已经手动建立 SSH 隧道时，在 Windows PowerShell 中执行：

```powershell
Set-Location D:\GoProjects\live-stream

.\scripts\load-test.ps1
```

默认进行 10 个连接、持续 30 秒的安全小规模测试。自定义参数示例：

```powershell
.\scripts\load-test.ps1 `
  -Connections 1000 `
  -ActiveClients 10 `
	-MaxLossPercent 0 `
  -SendInterval 1s `
  -RampInterval 2ms `
  -Warmup 3s `
  -Drain 5s `
  -Duration 2m
```

`-MaxLossPercent` 默认是 `0`，即任何投递缺口都会使测试失败；例如 `-MaxLossPercent 0.1` 允许最多 0.1% 的投递缺失。接收数大于期望投递数会按重复投递直接失败。压测消息携带纳秒发送时间，输出中的 `latency_p50`、`latency_p95`、`latency_p99` 来自固定桶直方图，不会随消息量保存延迟样本。

使用 `-MaxP95Latency 500ms` 可以把延迟纳入成功条件。省略该参数时只输出延迟、不按延迟判定；建议低负载基线使用 300～500ms，高连接数测试再根据业务目标调整。

可靠协议回归时可增加 `-AckEvery 50`。这会让每个监听客户端每处理约 50 条带 Kafka 游标的弹幕后发送一次最大游标 ACK；`room_seq` 对单房间递增但允许不连续。`0` 表示关闭 ACK，也是普通扇出压测的推荐值。ACK 当前不驱动服务端重传，应分别记录“关闭 ACK 的纯扇出测试”和“开启 ACK 的协议成本测试”。

热点房间已拆为 16 个客户端 shard。诊断接口新增：

- `ws_broadcast_duration`：编码并把一条消息分派到全部 shard 的耗时；
- `ws_fanout_shard_duration`：单个 shard 的快照和客户端入队耗时；
- `ws_resync_required_total`：实时队列出现缺口后发出的重同步提示；
- `ws_slow_client_disconnect_total`：持续队列满后主动关闭的客户端；
- `ws_control_queue_full_total`：高优先级控制队列也无法写入的次数。
- `ws_realtime_message_age`：消息到达 Redis Subscriber 时距离 Kafka 消息时间戳的时间；
- `/debug/kafka/realtime` 中的 `kafka_realtime_message_age`：realtime Consumer Fetch 时的消息年龄；
- `kafka_producer_write_duration`：WebSocket 收到弹幕后，Kafka RequireAll 写入完成所需时间；低流量时若接近 1 秒，应检查 Producer `BatchTimeout`；
- `kafka_realtime_redis_publish_duration`：Redis Publish 调用耗时；
- `kafka_realtime_commit_enqueue_duration`：offset 交给异步提交循环的耗时。

如果 `ws_fanout_shard_duration` 很低而 `ws_client_queue_depth` 持续增长，瓶颈位于 Socket、网络或压测机接收端，继续增加 fan-out worker 只会更快填满客户端队列。

`-ActiveClients 10` 表示只有 10 个连接携带登录 `sid` 并发送弹幕，其余 990 个连接按游客监听。也可以继续使用 `-ActiveRatio 0.01`，脚本会把它换算为确定数量；显式的 `-ActiveClients` 优先级更高。只测试建连时使用：

```powershell
.\scripts\load-test.ps1 `
  -Connections 1000 `
  -ActiveClients 0 `
  -Warmup 3s `
  -Drain 5s `
  -Duration 30s `
  -SkipHistoryCheck
```

压测器会等所有 Dial 完成后再开始发送。`Warmup` 用于让在线人数广播和连接写队列稳定，`Drain` 用于停止发送后继续接收 Kafka/Redis 返回的最后一批广播。

如果已经配置 SSH 密钥，可以让脚本自动建立和关闭隧道：

```powershell
.\scripts\load-test.ps1 `
  -StartTunnel `
  -SshTarget "yujun@119.91.119.208"
```

自动隧道使用 `BatchMode=yes`，不会读取或保存 SSH 密码。使用密码登录时，应先按照第 5 节手动建立隧道，再运行脚本。

IPv4 直连示例：

```powershell
.\scripts\load-test.ps1 `
  -BaseUrl "http://SERVER_IPV4:9000" `
  -WebSocketUrl "ws://SERVER_IPV4:9000/api/v1/ws" `
  -Connections 1000 `
  -ActiveClients 10 `
  -Warmup 3s `
  -Drain 5s `
  -Duration 2m
```

IPv6 直连时，WebSocket 地址中的 IPv6 必须放在方括号中：

```powershell
.\scripts\load-test.ps1 `
  -BaseUrl "http://[SERVER_IPV6]:9000" `
  -WebSocketUrl "ws://[SERVER_IPV6]:9000/api/v1/ws" `
  -Connections 1000 `
  -ActiveClients 10 `
  -Warmup 3s `
  -Drain 5s `
  -Duration 2m
```

SSH 隧道适合 10～100 个连接的功能验证。评估 1,000 个以上连接时应优先直连，并把 TCP 9000 的云安全组/防火墙来源限制为压测机公网 IP。

脚本只有在以下条件全部满足时才输出 `AUTOMATED LOAD TEST PASSED`：

- 注册、登录、创建和开播接口全部成功；
- benchmark 退出码为 0；
- benchmark 的 `errors` 为 0；
- Dial 成功数等于目标连接数；
- 发送模式下 `sent` 和 `received` 都大于 0；
- 接收数不大于期望投递数，且投递损失不超过 `MaxLossPercent`；
- 历史消息中能够查询到 `bench-*` 弹幕。
