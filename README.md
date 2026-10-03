# Live Stream

一个面向高并发场景的直播房间与实时弹幕系统。项目包含 Go 后端、React/Vinext 前端，以及 MySQL、Redis、Kafka 驱动的消息链路，支持用户与房间管理、实时弹幕、连续点赞、历史消息、断线补拉、热门排行和 WebSocket 压测。

> 当前项目聚焦直播互动链路，不包含真正的音视频推流服务；房间中的 `push_url` / `play_url` 是为接入 RTMP、HLS 等媒体服务预留的字段。

## 功能特性

- 用户注册、登录、退出，以及基于 HttpOnly Cookie 的 Session 鉴权
- 直播房间创建、编辑、开播、关播与热门列表
- 游客可观看弹幕，登录用户可发送弹幕和点赞
- Kafka 双 Consumer Group：实时广播与历史持久化相互解耦
- 同一房间内基于 Kafka offset 的递增 `room_seq`
- Redis Pub/Sub 支持多 WebSocket 网关实例间广播
- Redis 最近消息缓存与 MySQL 历史回源，支持断线补拉
- 弹幕 `message_id` 去重、点赞限流与批量持久化
- 慢客户端保护、热门房间分片扇出与自适应采样
- 基于在线人数、近期弹幕、近期点赞和开播时长的实时热度榜
- pprof、WebSocket/Kafka 运行指标、E2E 测试和分级压测脚本

## 技术栈

| 层级 | 技术 |
|---|---|
| 后端 | Go、Gin、GORM、Gorilla WebSocket |
| 前端 | React 19、Vinext、TypeScript、Tailwind CSS、shadcn/ui |
| 数据存储 | MySQL 8、Redis 7 |
| 消息队列 | Kafka 7.4、ZooKeeper |
| 工程化 | Docker Compose、Go Test、PowerShell/Bash 自动化测试 |

## 系统架构

```text
浏览器 / 压测客户端
        │ HTTP + WebSocket
        ▼
   Gin + WebSocket Hub
        │ 同步写入（RequireAll）
        ▼
      Kafka Topic
        ├── realtime consumer group
        │      ├── 弹幕 → Redis 最近消息缓存 + Pub/Sub → WebSocket Hub
        │      └── 点赞 → MySQL 聚合 → Redis 状态 → WebSocket Hub
        │
        └── persistence consumer group
               └── 弹幕历史 → MySQL 批量持久化
```

生产者以 `room_id` 作为 Kafka Key，使同一房间稳定落到同一分区。发送端收到 `accepted` 时表示消息已得到 Kafka 确认；实时展示不等待 MySQL，从而避免数据库抖动阻塞直播链路。消息格式和客户端交互方式见本文的“WebSocket 协议”章节及 [弹幕实时与恢复协议](docs/reliable-danmaku-protocol.md)。

## 快速开始

### 1. 环境要求

- Go `1.26.5`（以 [`go.mod`](go.mod) 为准）
- Docker 与 Docker Compose
- Node.js `>= 22.13.0`、pnpm（启动前端时需要）

默认端口：

| 服务 | 地址 |
|---|---|
| 后端 API / WebSocket | `http://127.0.0.1:9000` |
| 前端开发服务 | `http://localhost:3000` |
| MySQL | `127.0.0.1:3306` |
| Redis | `127.0.0.1:6379` |
| Kafka | `127.0.0.1:9092` |
| 诊断与指标 | `http://127.0.0.1:6060` |

### 2. 启动基础设施

```bash
docker compose up -d
docker compose ps
```

Compose 会启动 MySQL、Redis、Kafka 和 ZooKeeper，并创建 `live_stream` 数据库。仓库中的账号密码仅用于本地开发；部署前请同步修改 [`docker-compose.yml`](docker-compose.yml) 与 [`config/config.yaml`](config/config.yaml)。

### 3. 启动后端

```bash
go mod download
go run .
```

服务启动时会自动执行 GORM 数据表迁移、恢复点赞计数，并启动 Kafka 消费者、热度榜任务、Redis 订阅器和诊断服务。

可用下面的命令确认后端已经就绪：

```bash
curl http://127.0.0.1:9000/api/v1/rooms
```

### 4. 启动前端

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm dev
```

浏览器打开 `http://localhost:3000`。开发服务器会将 `/api` 和 WebSocket 请求同源代理到 `http://127.0.0.1:9000`，避免 Session Cookie 被跨站策略拦截。

如果后端运行在另一台机器，可创建 `frontend/.env.local`：

```dotenv
LIVE_STREAM_BACKEND_URL=http://192.168.1.10:9000
LIVE_STREAM_WEBSOCKET_ORIGIN=http://localhost:3000
```

`LIVE_STREAM_WEBSOCKET_ORIGIN` 必须与后端 `server.allowed_origins` 中的一项完全一致。局域网部署示例和 Cookie 注意事项见 [前端说明](frontend/README.md)。

## 配置

后端配置位于 [`config/config.yaml`](config/config.yaml)，主要分为：

| 配置段 | 作用 |
|---|---|
| `mysql` / `redis` / `kafka` | 基础设施连接参数 |
| `stream_list.count` | 房间列表默认数量 |
| `server.allowed_origins` | HTTP 与 WebSocket Origin 白名单 |
| `server.cookie_*` | Session Cookie 的 Secure / SameSite 策略 |
| `danmaku.recent_cache` | 最近弹幕缓存数量和 TTL |
| `danmaku.hot_room` | 热门房间阈值、扇出预算与采样上限 |
| `danmaku.hot_ranking` | 热度榜刷新周期、指标 TTL 与权重 |
| `danmaku.slow_client` | 队列高水位和慢连接断开策略 |
| `danmaku.like` | 点赞限流、批量落库与幂等窗口 |

本地 HTTP 开发应保持 `cookie_secure: false`。HTTPS 生产环境应设置为 `true`；跨站部署还需将 `cookie_same_site` 调整为 `none`，并正确配置允许的 Origin。

## API 概览

所有 HTTP 接口均以 `/api/v1` 开头。带“登录”标记的接口依赖登录后写入的 `sid` Cookie。

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| `POST` | `/auth/register` | 注册用户 | 公开 |
| `POST` | `/auth/login` | 登录并设置 Session Cookie | 公开 |
| `GET` | `/auth/me` | 获取当前用户 | 登录 |
| `POST` | `/auth/logout` | 退出登录 | 登录 |
| `GET` | `/rooms` | 获取直播房间列表 | 公开 |
| `GET` | `/rooms/mine` | 获取当前用户的房间 | 登录 |
| `GET` | `/rooms/:room_id` | 获取房间详情 | 公开 |
| `POST` | `/rooms` | 创建房间 | 登录 |
| `PATCH` | `/rooms/:room_id` | 修改房间标题 | 房主 |
| `POST` | `/rooms/:room_id/start` | 开播 | 房主 |
| `POST` | `/rooms/:room_id/stop` | 关播 | 房主 |
| `GET` | `/rooms/:room_id/messages` | 按历史 ID 分页查询弹幕 | 公开 |
| `GET` | `/rooms/:room_id/messages/replay` | 按 `room_seq` 补拉弹幕 | 公开 |
| `GET` | `/ws?room_id=:room_id` | 建立直播间 WebSocket | 公开可读，登录可写 |

注册、登录并创建房间的最小示例：

```bash
curl -X POST http://127.0.0.1:9000/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"demo","email":"demo@example.com","password":"Demo12345"}'

curl -c cookies.txt -X POST http://127.0.0.1:9000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@example.com","password":"Demo12345"}'

curl -b cookies.txt -X POST http://127.0.0.1:9000/api/v1/rooms \
  -H "Content-Type: application/json" \
  -d '{"title":"Demo Live"}'
```

## WebSocket 协议

连接地址：

```text
ws://127.0.0.1:9000/api/v1/ws?room_id=1
```

发送弹幕时建议由客户端生成 UUID，并在重试同一条逻辑消息时复用：

```json
{
  "type": "danma",
  "message_id": "91a13b9a-a67e-49f5-98d4-3e747c42d8d1",
  "content": "hello"
}
```

发送点赞：

```json
{
  "type": "like",
  "message_id": "f0ac82e2-ea75-4e0a-94d7-6d96f7a69ea5"
}
```

服务端会根据连接和 Session 覆盖客户端提交的 `room_id`、`user_id` 与 `user_name`，不能通过消息体伪造身份。常见服务端消息包括：

- `accepted`：Kafka 已确认接收；
- `danma`：实时弹幕，包含 `message_id` 与 `room_seq`；
- `system/room_state`：在线人数和点赞数；
- `system/delivery_policy`：推送或采样模式变更；
- `resync_required`：客户端需要从指定游标补拉；
- `error`：参数、鉴权、限流等错误。

## 测试与压测

运行后端单元测试与静态检查：

```bash
go test ./...
go vet ./...
```

运行前端检查：

```bash
cd frontend
pnpm lint
pnpm build
```

基础设施和后端启动后，可以执行全套功能测试：

```powershell
# Windows
.\scripts\full-test.ps1 -Profile Functional
```

```bash
# Linux
bash ./scripts/full-test.sh --profile Functional --skip-frontend-checks
```

测试脚本提供以下执行档位：

| Profile | 覆盖范围 |
|---|---|
| `Functional` | API、Session、房间权限、WebSocket、持久化、历史消息与点赞 |
| `Smoke` | 功能检查及 100 个连接的基础压测 |
| `Standard` | 依次测试 100 和 1,000 个连接 |
| `Stress` | 依次测试 1,000 和 5,000 个连接 |
| `Extreme` | 最高测试 10,000 个连接 |

`Stress` 和 `Extreme` 会消耗大量 CPU、网络和数据库资源，只应在专用环境运行。可通过下面的命令查看脚本参数：

```powershell
Get-Help .\scripts\full-test.ps1 -Detailed
```

```bash
bash ./scripts/full-test.sh --help
```

每次全量测试会在 `test-results/` 中生成 JSON 和 Markdown 报告；功能断言或压测阈值失败时，脚本会返回非零退出码。

## 可观测性

诊断服务只监听本机 `127.0.0.1:6060`：

```bash
curl http://127.0.0.1:6060/debug/ws/metrics
curl http://127.0.0.1:6060/debug/kafka/realtime
```

pprof 入口为 `http://127.0.0.1:6060/debug/pprof/`。核心指标覆盖广播耗时、分片扇出耗时、客户端队列深度、丢弃消息、重同步、主动断开、自适应采样，以及点赞批处理数量与持久化耗时。

## 项目结构

```text
.
├── main.go                    # 后端入口与生命周期管理
├── config/                    # 后端配置
├── internal/
│   ├── auth/                  # Session
│   ├── config/                # 配置加载
│   ├── dao/                   # MySQL、Redis 与迁移
│   ├── handler/               # HTTP Handler
│   ├── hotrank/               # 实时热度榜
│   ├── kafka/                 # Producer、实时/持久化 Consumer
│   ├── middleware/            # 鉴权与 CORS
│   ├── model/                 # 数据模型与 DTO
│   ├── repo/                  # 数据访问层
│   ├── service/               # 业务逻辑
│   └── ws/                    # WebSocket Hub、客户端与扇出策略
├── frontend/                  # React/Vinext 前端
├── cmd/benchmark/             # WebSocket 压测工具
├── cmd/e2e/                   # WebSocket 协议 E2E 工具
├── scripts/                   # 自动化测试与压测脚本
├── web/danmaku-client.js      # 独立浏览器客户端参考实现
└── docs/                      # 开发、协议与测试文档
```

## 相关说明

- [前端启动与局域网配置](frontend/README.md)
- [WebSocket 压测工具](cmd/benchmark/README.md)
- [后端开发指南](docs/backend-development-guide.md)
- [弹幕实时与恢复协议](docs/reliable-danmaku-protocol.md)
- [负载测试指南](docs/load-testing-guide.md)
- [全套自动化测试指南](docs/testing/full-automated-test-guide.md)
