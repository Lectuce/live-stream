# Live Stream

这是一个把视频、直播和社区内容放在同一套账号体系里的 Web 项目。用户可以投稿视频、开直播、发布图文，其他用户可以关注作者、刷 Feed、发弹幕、评论、点赞和收藏。管理员负责稿件审核、评论审核与举报处理。

项目最早从直播弹幕链路开始，后来逐步补齐了视频投稿、内容审核、推荐 Feed、关注动态、视频弹幕和社区互动。目前核心流程已经可以在本地或单机服务器上跑通，适合继续做功能开发、链路压测和工程化改造。

> 当前媒体链路采用单节点 SRS + HLS，没有做转码、多码率、自适应清晰度和 CDN 分发。它可以完成真实推流与播放联调，但还不是可直接承载大规模公网流量的部署方案。

## 已实现的功能

### 视频

- 视频上传、编辑、送审、审核、发布、下架和软删除
- 创作中心与管理员审核页，审核记录带稿件版本，避免旧版本审核结果覆盖新内容
- 首页推荐、热门、分区和最新视频 Feed，使用快照游标分页，减少翻页时的重复和遗漏
- 视频播放、观看历史、播放进度、点赞、收藏和行为上报
- 视频时间轴弹幕，支持按时间窗口加载、发送、屏蔽和播放器进度同步

### 直播

- 直播房间创建、编辑、开播、关播和状态查询
- OBS 通过 RTMP 推流，SRS 输出 HLS，网页端使用 HLS.js 播放
- 每次开播生成独立场次和短期推流凭证；SRS 回调负责确认开播、停播，后台任务负责状态对账
- 热门直播 Feed、关注直播和开播动态
- WebSocket 实时弹幕、连续点赞、断线补拉和房间热度排行
- 慢连接队列、热点房间分片广播和过载采样，避免单个连接拖慢整个房间

### 社区

- 图文发布、编辑、删除、图片上传和详情页
- 视频与图文共用两级评论体系，支持回复、排序、点赞、删除和举报
- 关注与取消关注、作者主页、关注动态
- 站内通知、举报处理、评论审核和管理员入口
- 搜索用户、视频和直播房间

## 关键链路

### 内容发布与关注动态

```text
视频 / 图文 / 开播事务
        │
        ├── 写入业务数据
        └── 写入 outbox_events
                    │
              Outbox Worker
                    │
                    ▼
          Kafka content-events
                    │
                    ▼
        Following Feed Projector
                    │
                    ▼
               feed_events
```

业务数据和 Outbox 事件在同一事务中提交。Kafka 允许重复投递，消费者依靠事件唯一键幂等写入，避免把消息系统误当成天然的“恰好一次”。

### 直播与实时弹幕

```text
OBS ──RTMP──> SRS ──HLS──> Nginx ──> 浏览器播放器
               │                         │
               └── HTTP Callback ──> Go 后端

浏览器 ──WebSocket──> Hub ──> Kafka
                                  ├── 实时消费 ──> Redis Pub/Sub ──> Hub
                                  └── 持久化消费 ──> MySQL
```

实时广播和持久化使用不同的 Consumer Group。消息按房间分区，既保留房间内顺序，也不会让数据库写入阻塞实时推送。短期历史优先从 Redis 补拉，缓存范围外再回源 MySQL。

## 技术栈

| 部分 | 技术 |
| --- | --- |
| 后端 | Go、Gin、GORM |
| 前端 | React 19、TypeScript、Vinext、Tailwind CSS、shadcn/ui |
| 数据 | MySQL 8、Redis 7 |
| 消息 | Kafka、ZooKeeper |
| 实时通信 | WebSocket |
| 直播媒体 | SRS、RTMP、HLS、HLS.js |
| 部署与代理 | Docker Compose、Nginx |

## 本地启动

### 环境要求

- Go 1.26.5
- Docker 与 Docker Compose
- Node.js 22.13 或更高版本
- pnpm
- FFmpeg（视频上传校验需要 `ffprobe`，真实媒体验收脚本需要 `ffmpeg`）

### 1. 启动基础设施

```bash
docker compose up -d
docker compose ps
```

这会启动 MySQL、Redis、Kafka 和 ZooKeeper。数据保存在 Docker Volume 中，停止容器不会自动删除。

### 2. 启动后端

```bash
go mod download
go run .
```

后端监听 `http://127.0.0.1:9000`，启动时会自动创建和更新开发环境所需的数据表。Kafka、MySQL 或 Redis 未就绪时，后端可能直接退出；先用 `docker compose ps` 和容器日志确认依赖状态。

### 3. 启动前端

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm dev --hostname 0.0.0.0 --port 3000
```

本机访问 `http://localhost:3000`。前端通过同源 `/api` 和 `/ws` 代理后端，因此浏览器不需要直接访问 9000 端口。

如果前后端不在同一台机器，在 `frontend/.env.local` 中设置后端地址和浏览器实际使用的前端 Origin：

```dotenv
LIVE_STREAM_BACKEND_URL=http://127.0.0.1:9000
LIVE_STREAM_WEBSOCKET_ORIGIN=http://localhost:3000
```

`LIVE_STREAM_WEBSOCKET_ORIGIN` 还需要出现在 `config/config.yaml` 的 `server.allowed_origins` 中。公网部署应由 Nginx 暴露前端、API、WebSocket 和媒体地址，不要直接开放数据库、Redis、Kafka、SRS 管理 API 或 Go 诊断端口。

## 启用真实直播

普通的视频、图文和社区功能不依赖 SRS。需要用 OBS 真实推流时，再启用媒体服务。

先为后端设置环境变量：

```bash
export LIVE_MEDIA_SECRET="$(openssl rand -hex 32)"
export LIVE_PUSH_BASE=rtmp://你的域名或公网IP:1935/live
export LIVE_PLAY_BASE=/live-media
export LIVE_SRS_API=http://127.0.0.1:1985
```

然后启动 SRS：

```bash
docker compose -f deploy/live-media/compose.yaml up -d
docker compose -f deploy/live-media/compose.yaml logs --tail=50 srs
```

Nginx 需要代理 HLS 文件，公网安全组需要放行主播连接使用的 TCP 1935。SRS 的 1985、8080、9001 只供本机组件使用，不应直接暴露到公网。完整配置、OBS 参数和验收步骤见 [真实直播部署说明](deploy/live-media/README.md)。

登录作者账号后打开 `/live#studio`，创建房间并点击开始直播。页面会给出 OBS 的服务器地址和推流密钥；OBS 真正连上 SRS 且回调通过后，房间才会变成“直播中”。

## 常用页面

| 地址 | 用途 |
| --- | --- |
| `/` | 视频推荐首页 |
| `/popular`、`/category/:id` | 热门与分区视频 |
| `/video/:id` | 视频播放、弹幕与评论 |
| `/upload`、`/studio` | 投稿与创作中心 |
| `/live`、`/feed` | 直播首页与直播 Feed |
| `/rooms/:id` | 直播间 |
| `/dynamic` | 关注动态 |
| `/posts`、`/posts/:id` | 图文社区 |
| `/space/:id` | 作者主页 |
| `/messages` | 站内通知 |
| `/admin/videos` | 视频审核 |
| `/admin/comments`、`/admin/reports` | 评论审核与举报处理 |

管理员角色必须由服务端或数据库设置，注册接口不会接受客户端指定的管理员身份。

## 配置

基础配置在 `config/config.yaml`：

- MySQL、Redis、Kafka 的连接地址
- WebSocket 允许的 Origin
- 最近弹幕缓存、慢连接阈值和点赞批处理参数
- 热门房间采样与热度排行参数
- Cookie 的 Secure 和 SameSite 策略

常用环境变量：

| 变量 | 作用 |
| --- | --- |
| `VIDEO_STORAGE_DIR` | 视频文件保存目录 |
| `POST_IMAGE_STORAGE_DIR` | 图文图片保存目录 |
| `VIDEO_UPLOAD_AUTO_APPROVE=1` | 开发时自动通过视频审核，不建议在正式环境开启 |
| `VIDEO_DEMO_MODE=1` | 使用演示视频模式 |
| `CONTENT_EVENTS_TOPIC` | 内容事件 Kafka Topic，未设置时使用默认值 |
| `LIVE_MEDIA_SECRET` | 直播推流凭证签名密钥；设置后启用真实直播 |
| `LIVE_PUSH_BASE` | OBS 使用的 RTMP 基础地址 |
| `LIVE_PLAY_BASE` | 浏览器使用的 HLS 基础地址 |
| `LIVE_SRS_API` | SRS 管理 API 地址 |

上传大小还会受 Nginx `client_max_body_size` 限制。出现 HTTP 413 时，应同时检查 Nginx 和后端的上传限制，而不是只改前端。

## 测试与验收

后端基础检查：

```bash
go test ./...
go vet ./...
```

前端检查：

```bash
cd frontend
pnpm lint
pnpm build
```

基础设施和后端已经运行时，可以执行原有的接口与弹幕链路测试：

```powershell
# Windows
.\scripts\full-test.ps1 -Profile Functional
```

```bash
# Linux
bash ./scripts/full-test.sh --profile Functional --skip-frontend-checks
```

`scripts/` 下保留了各阶段的接口验收脚本，包括关注动态、评论、互动、视频弹幕、观看历史、视频 Feed 和真实直播。它们会创建或修改测试数据，运行前先阅读参数说明，并尽量使用单独的开发数据库和测试账号。

常用脚本：

```text
scripts/test-following-feed.py    关注动态
scripts/test-l4a-comments.py      评论与回复
scripts/test-l4b-interactions.py  点赞、收藏和举报
scripts/test-l4c-danmaku.py       视频弹幕
scripts/test-l4d-watch-history.py 观看历史
scripts/test-l5-video-feed.py     视频 Feed
scripts/test-l6-live-media.py     真实直播
scripts/seed-live-feed.sh         批量创建直播 Feed 测试数据
```

## 诊断与压测

后端诊断服务只监听 `127.0.0.1:6060`：

- `/debug/pprof/`
- `/debug/ws/metrics`
- `/debug/kafka/realtime`

需要远程查看时使用 SSH 端口转发，不要把 6060 暴露到公网。

```bash
ssh -L 6060:127.0.0.1:6060 user@server
```

弹幕压测工具位于 `cmd/benchmark`，基础用法：

```bash
go run ./cmd/benchmark \
  -urls ws://127.0.0.1:9000/api/v1/ws \
  -origin http://benchmark.local \
  -room 1 \
  -c 100 \
  -d 30s
```

## 目录结构

```text
.
├── main.go                    后端入口与后台任务启动
├── config/                    本地配置
├── internal/
│   ├── handler/               HTTP 与 WebSocket 接口
│   ├── service/               业务逻辑
│   ├── repo/                  数据访问与事务
│   ├── model/                 数据模型
│   ├── ws/                    连接管理与房间广播
│   ├── kafka/                 实时、持久化与内容事件消费者
│   ├── outbox/                Outbox 投递任务
│   └── hotrank/               直播热度排行
├── frontend/                  React 前端
├── deploy/live-media/         SRS 与 Nginx 示例配置
├── scripts/                   接口验收、造数与测试脚本
└── cmd/benchmark/              WebSocket 与 Kafka 压测工具
```

## 目前的边界

- 数据表仍由 GORM AutoMigrate 管理，正式部署前应改成可审计、可回滚的版本化迁移。
- 直播是单 SRS 节点、单路 HLS，没有转码、录制、多码率、CDN 和跨节点容灾。
- HLS 有数秒延迟；需要更低延迟时应单独评估 WebRTC 或低延迟 HLS。
- 当前配置面向开发环境。密钥管理、HTTPS、对象存储、限流、内容安全、监控告警和备份恢复仍需按部署环境补齐。
- 小带宽服务器同时观看人数有限。直播码率、并发观众数和出口带宽必须一起估算，不能只看 CPU 与内存。

如果只是体验功能，先启动基础设施、后端和前端即可；如果要验证真实直播，再按媒体部署说明启动 SRS 和 Nginx 代理。
