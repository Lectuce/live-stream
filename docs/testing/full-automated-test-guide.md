# live-stream 全套自动化测试指南

入口脚本：

- Windows：`scripts/full-test.ps1`
- Linux：`scripts/full-test.sh`（原生 Bash，不需要 PowerShell/pwsh）

该脚本依据 `docs/deep-dive/2026-09-02-live-stream-project-review.md` 建立测试矩阵，覆盖静态检查、HTTP API、Session、房间权限、WebSocket 协议、Kafka 持久化、历史分页、断线补拉、点赞状态以及分级压测。

## 测试前提

1. 后端、MySQL、Redis、Kafka 已启动。
2. Windows 能访问后端的 HTTP/WebSocket 地址。
3. 后端 `server.allowed_origins` 包含测试传入的 Origin，例如 `http://localhost:5173`。
4. 本机已安装 Go。若要检查前端，还需要 Node.js 和 pnpm。
5. 测试会创建随机用户和房间，并在结束时关播；当前项目没有删除用户/房间接口，所以测试数据会保留。

## Linux 单机测试

Ubuntu/Linux 上不要安装 PowerShell，直接执行 Bash 版本。先确认依赖：

```bash
command -v bash curl python3 go
curl -f http://127.0.0.1:9000/api/v1/rooms
```

第一次先跑完整功能测试，不做连接压测：

```bash
cd ~/projects/live-stream

bash ./scripts/full-test.sh \
  --base-url http://127.0.0.1:9000 \
  --websocket-url ws://127.0.0.1:9000/api/v1/ws \
  --origin http://localhost:5173 \
  --diagnostics-base-url http://127.0.0.1:6060 \
  --profile Functional \
  --like-rate-limit 10 \
  --skip-frontend-checks
```

功能测试通过后，依次执行烟雾、标准和压力测试：

```bash
# 100 个连接、1 个发送者
bash ./scripts/full-test.sh \
  --profile Smoke \
  --max-loss-percent 0 \
  --max-p95-latency 200ms \
  --skip-frontend-checks

# 依次测试 100×1 和 1000×10
bash ./scripts/full-test.sh \
  --profile Standard \
  --max-loss-percent 0 \
  --max-p95-latency 500ms \
  --skip-frontend-checks

# 依次测试 1000×10 和 5000×10
bash ./scripts/full-test.sh \
  --profile Stress \
  --max-loss-percent 0 \
  --skip-frontend-checks

# 最后增加 10000×10，仅在专用压测环境运行
bash ./scripts/full-test.sh \
  --profile Extreme \
  --max-loss-percent 0 \
  --skip-frontend-checks
```

本机默认地址已经是 `127.0.0.1`，因此也可以简写为：

```bash
bash ./scripts/full-test.sh --profile Functional --skip-frontend-checks
```

若想直接执行脚本，可先设置一次执行权限：

```bash
chmod +x ./scripts/full-test.sh
./scripts/full-test.sh --profile Functional --skip-frontend-checks
```

注意：命令中的 URL 必须是纯文本 URL，不能复制为 `[http://...](http://...)` 这种 Markdown 链接。

## Windows / 局域网测试

先做连通性检查：

```powershell
Test-NetConnection 172.21.15.57 -Port 9000
Invoke-RestMethod -Method Get -Uri "http://172.21.15.57:9000/api/v1/rooms"
```

## 推荐执行方式

第一次只跑功能测试：

```powershell
Set-Location D:\GoProjects\live-stream

PowerShell -ExecutionPolicy Bypass -File .\scripts\full-test.ps1 `
  -BaseUrl "http://172.21.15.57:9000" `
  -WebSocketUrl "ws://172.21.15.57:9000/api/v1/ws" `
  -Origin "http://localhost:5173" `
  -Profile Functional
```

功能通过后执行默认烟雾压测（100 个连接、1 个发送者）：

```powershell
PowerShell -ExecutionPolicy Bypass -File .\scripts\full-test.ps1 `
  -BaseUrl "http://172.21.15.57:9000" `
  -WebSocketUrl "ws://172.21.15.57:9000/api/v1/ws" `
  -Origin "http://localhost:5173" `
  -Profile Smoke `
  -MaxLossPercent 0 `
  -MaxP95Latency 200ms
```

标准压测包含 100×1 和 1,000×10 两个阶段：

```powershell
PowerShell -ExecutionPolicy Bypass -File .\scripts\full-test.ps1 `
  -BaseUrl "http://172.21.15.57:9000" `
  -WebSocketUrl "ws://172.21.15.57:9000/api/v1/ws" `
  -Origin "http://localhost:5173" `
  -Profile Standard `
  -MaxLossPercent 0 `
  -MaxP95Latency 500ms
```

`Stress` 会测试 1,000 和 5,000 个连接；`Extreme` 还会测试 10,000 个连接。它们会产生明显的 CPU、网络、Kafka 和数据库负载，只应在专用测试环境执行：

```powershell
.\scripts\full-test.ps1 `
  -BaseUrl "http://172.21.15.57:9000" `
  -WebSocketUrl "ws://172.21.15.57:9000/api/v1/ws" `
  -Origin "http://localhost:5173" `
  -Profile Extreme `
  -MaxLossPercent 0
```

如果服务器上的诊断端口通过 SSH 转发到了本机 16060，可同时采集测试前后的指标：

```powershell
ssh -N -L 16060:127.0.0.1:6060 yujun@172.21.15.57

.\scripts\full-test.ps1 `
  -BaseUrl "http://172.21.15.57:9000" `
  -WebSocketUrl "ws://172.21.15.57:9000/api/v1/ws" `
  -DiagnosticsBaseUrl "http://127.0.0.1:16060" `
  -Profile Standard
```

Kafka 纯写入压测默认关闭，传入 broker 后才执行：

```powershell
.\scripts\full-test.ps1 `
  -BaseUrl "http://127.0.0.1:9000" `
  -WebSocketUrl "ws://127.0.0.1:9000/api/v1/ws" `
  -Profile Smoke `
  -KafkaBrokers "127.0.0.1:9092" `
  -KafkaMessages 100000 `
  -KafkaWorkers 8
```

## 覆盖范围

| 模块 | 自动检查 |
|---|---|
| Go 代码 | `go test ./...`、`go vet ./...` |
| 前端 | `pnpm lint`、`pnpm build`（存在 `frontend/package.json` 时） |
| 注册 | DTO 校验、成功注册、重复邮箱、自动生成 user_id |
| 登录与 Session | 错误密码、sid 格式、`/auth/me`、logout 后失效 |
| 房间 | 创建、单用户单房间、mine、详情、改名、开播、关播、榜单 |
| 权限 | 未登录访问、非房主修改/开播/关播 |
| WebSocket | Origin、离线房间、游客读、游客禁止写、登录用户发送 |
| 消息安全 | 服务端覆盖伪造的 room_id/user_id/user_name，UUID 生成与校验 |
| 实时链路 | accepted、Kafka→Redis→WebSocket、两名观众顺序一致 |
| ACK | 客户端发送 ACK，连接保持可用 |
| 持久化 | Kafka 持久化消费者写入 MySQL，message_id 可查 |
| 历史 | limit、has_more、游标分页、无效参数 |
| 补拉 | `after_seq`、递增 room_seq、Redis/MySQL source |
| 点赞 | accepted、实时 room_state、重复 message_id 行为 |
| 热度榜 | 公式与时间衰减、分钟桶近似、Redis 排名顺序、按开播时间降级的源码回归 |
| 压测 | 建连成功率、峰值连接、发送/投递、丢失率、P50/P95/P99、历史落库 |

## 报告与退出码

每次执行都会在 `test-results/` 生成：

- `full-test-时间.json`：适合 CI 和程序读取；
- `full-test-时间.md`：适合人工查看和项目复盘。

Linux 脚本对应生成 `full-test-linux-时间.json` 和 `full-test-linux-时间.md`。

功能断言或压测阈值失败时脚本返回非零退出码。已确认但尚未修复的架构问题默认以 `WARN` 写入报告，不会中断其他测试。若希望 CI 对这些问题也失败，增加：

```powershell
-FailOnKnownIssues
```

Linux 对应参数为：

```bash
--fail-on-known-issues
```

## 点赞一致性修复及回归检查

脚本会同时做源码审计和运行时重复点赞检查。以下三个历史问题已经修复，并会持续回归验证：

1. realtime consumer 按 100 条或 200 ms 聚合点赞，每个房间/分区窗口只写一条 `like_batches(delta)`，并且只更新一次 `rooms` 热点行。
2. 聚合记录、`rooms.like_count += delta` 和 Kafka 分区 checkpoint 位于同一个 MySQL 事务；即使重启后批次边界改变，已处理 offset 也不会再次计数。
3. `rooms.like_count` 是权威计数，Redis 只保存实时镜像；MySQL 成功后严格按 Redis、广播、Kafka offset 的顺序推进。
4. 启动时会保留旧 Redis 较大值并迁回 MySQL，此后每隔配置周期从 MySQL 重建 Redis；Redis 重启或清空后能够恢复。

用户仍然可以连续点赞，但单个用户对单个房间默认限制为每秒 10 次。不同 `message_id` 是不同的点赞；相同 `message_id` 在配置的 Redis 幂等窗口内只会进入 Kafka 一次。Kafka 自身的消费重放由 MySQL checkpoint 永久吸收。

房间排序与累计点赞已经解耦：`like_count` 只用于展示，`hot_score` 使用 Redis 的近期点赞、近期弹幕、全局在线连接和开播时间衰减计算。同一用户每分钟默认最多贡献 10 次热度点赞；Redis 榜单不可用时接口按 `start_at DESC, room_id DESC` 降级。源码审计会阻止列表接口重新退回 `like_count DESC`。

脚本不会自动停止 Redis/Kafka/MySQL 来做故障注入，因为这会影响共享环境并可能破坏数据。服务重启、Redis 清空、Kafka 重放、MySQL 故障等测试应在独立 Docker 测试栈中另行执行。
