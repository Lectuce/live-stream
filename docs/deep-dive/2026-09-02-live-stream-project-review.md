# [项目深挖] live-stream - 2026-09-02

> 文档定位：基于当前仓库源码、测试、提交历史和已有压测记录形成的技术复盘。
>
> 证据标签：
>
> - 【源码事实】：当前代码可以直接证明。
> - 【历史实测】：仓库文档记录过，但最新架构改造后尚未重新完成同等规模全链路复测。
> - 【设计建议】：当前尚未实现，是面试时应主动承认的演进方向。
> - 【待本人补证】：源码无法证明，不能替本人虚构。

## 1. 先给结论

live-stream 是一个 Go 直播互动后端，完成了用户 Session、直播房间、WebSocket 弹幕、Kafka 可靠交接、Redis 实时分发与最近消息补偿、MySQL 历史持久化，以及慢客户端治理、热房自适应推拉和端到端压测。

它最有价值的地方不是“用了很多中间件”，而是围绕实时系统的三个矛盾做了工程化处理：

1. 实时性与可靠性：实时展示不能等待 MySQL，但消息又要能落历史。
2. 完整性与可用性：慢客户端不应拖垮整个房间，但丢弃后必须可补偿。
3. 单房间扇出与资源上限：输入 QPS 很低时，乘以在线人数后仍可能形成巨大的投递量。

当前项目适合定位为“具有可靠消息、背压和容量验证意识的直播互动后端原型”，不能定位为完整直播平台或生产级高可用系统。它没有真实音视频推流链路、内容审核、限流、跨地域容灾、完整监控告警和真实业务用户。

### 1.1 完成度判断

| 维度 | 当前判断 | 证据或边界 |
|---|---|---|
| 功能闭环 | 较完整 | 注册登录、房间管理、开关播、实时弹幕、历史与补拉均有接口 |
| 四层架构 | HTTP 面清晰 | Handler → Service → Repo → DAO；实时面是独立事件管道 |
| 实时链路 | 较深入 | Kafka 双消费组、Redis Pub/Sub、房间分片扇出、PreparedMessage |
| 可靠性 | 有设计但非绝对可靠 | 至少一次、消息去重、补拉；仍有 Pub/Sub 静默缺口和退出恢复问题 |
| 性能工程 | 有证据链 | 自研 benchmark、严格丢失率判定、延迟分位、pprof 与运行指标 |
| 测试 | 核心并发模块较好，业务层偏弱 | ws 覆盖约 55.9%，Service/Repo/DAO/Handler 基本无自动测试 |
| 生产化 | 未完成 | 单 Kafka Broker、明文配置、无限流审核、无服务自愈、无部署编排 |
| 业务价值 | 尚无商业数据 | 只能讲工程价值，不能虚构 DAU、收入或降本比例 |

## 2. 面试开场

### 2.1 30 秒版本

我做了一个基于 Go 的直播房间和实时弹幕后端。HTTP 业务面按 Handler、Service、Repo、DAO 分层，实时弹幕通过 WebSocket 接入，以 room_id 作为 Kafka Key 保证房间内有序，再用两个 Consumer Group 分离实时广播和 MySQL 持久化。实时组把消息原子写入 Redis 最近消息 ZSet 后发布 Pub/Sub，各实例再按房间和客户端分片广播。针对慢客户端和热房，我实现了有界队列、丢旧保新、resync 补拉以及按扇出预算自适应采样，并用自研压测工具验证连接数、投递量、延迟和丢失率。

### 2.2 3 分钟完整工程故事

这个项目起点不是单纯做房间 CRUD，而是想把直播弹幕从“WebSocket 收到后遍历连接发送”的演示，推进到一个能讨论可靠性、顺序、背压和容量边界的工程原型。

第一阶段我完成用户、房间和会话闭环。密码使用 Argon2id，服务端生成随机 Session Token，只把 Token 哈希存入 Redis；房间创建、修改、开播和关播都从 Session 获取用户身份并校验房主权限。HTTP 部分采用 Handler、Service、Repo、DAO 四层，分别负责协议、业务规则、查询语义和基础设施。

第二阶段是弹幕主链路。登录用户通过 WebSocket 发消息，服务端覆盖客户端伪造的 room_id、user_id 和 username，并生成或校验 message_id。Producer 使用 room_id 作为 Key，通过 Hash Balancer 写入 Kafka，RequiredAcks 为 RequireAll。Kafka 是可靠交接点，发送成功后只返回 accepted，不代表观众已收到或 MySQL 已落库。

Kafka 后面拆成两个 Consumer Group：实时组先把弹幕通过 Lua 原子写入 Redis ZSet、裁剪、续 TTL，再发布 Pub/Sub，成功后才推进实时组 offset；持久化组把弹幕事务写入 MySQL，成功后再提交自己的 offset。这样 MySQL 抖动只增加持久化 Lag，不阻塞直播间实时展示。重复消费通过 message_id 唯一索引和客户端去重承受。

第三阶段是扇出和过载治理。Hub 按 room_id 管理连接，一个房间再按连接 ID 分成 16 个 shard，每个 room+shard 固定映射到 FIFO worker；这样不同 shard 可以并行，同一客户端的消息仍不乱序。同一条广播只序列化一次并复用 PreparedMessage。每个客户端有事件、控制和可覆盖状态三类队列：在线人数和点赞总数只保留最新值，控制消息不被旧弹幕阻塞；慢客户端队列高水位后丢旧保新并发 resync_required，持续积压才断开。

第四阶段是推拉结合。每条弹幕有 room_seq，当前实现直接使用 Kafka partition offset+1。客户端断线、收到 resync 或进入热房采样模式后，用 after_seq 从 Redis 最近窗口补拉，窗口覆盖不到再回源 MySQL，并按 message_id 去重。热房根据源 QPS×本机连接数估算扇出，超过预算时只推部分消息，其余由客户端错峰轮询补齐。

最后我没有只报“几万 QPS”，而是把连接数、输入消息率、广播投递率、延迟和缺口分开。历史测试证明过 10,000 连接可全量建连，旧版本在 1,000 连接、约 9,800 次投递/秒下无损；更高扇出出现过不同程度缺口。最新推拉和自适应版本尚未完成相同环境复测，因此面试时我会把历史数据和当前代码能力分开讲。

## 3. 为什么做：业务问题与技术动作

### 3.1 直接原因

【源码与历史文档事实】项目要解决直播互动中最基本的闭环：

- 用户能注册、登录并维持身份。
- 主播能创建房间、修改标题、开播和关播。
- 游客能观看，登录用户能发送弹幕或点赞。
- 弹幕既要尽快出现在观众端，又要可持久化、可翻页、可断线恢复。
- 热门房间、集中建连和慢客户端不能把服务拖垮。

### 3.2 根本原因

直播互动不是普通 CRUD。它具有长连接、一对多放大、瞬时热点、部分用户网络慢和跨实例广播等特征。如果沿用“请求到来 → 写 MySQL → 遍历所有连接同步发送”的直线方案，会把数据库延迟、单个慢连接和房间规模全部耦合到用户体验上。

因此核心设计目标不是追求绝对不丢，而是明确消息语义：

- Kafka 负责耐久交接和解耦。
- Redis Pub/Sub 负责低延迟跨实例通知。
- Redis ZSet 与 MySQL 负责不同时间尺度的补偿。
- WebSocket Hub 负责本机房间路由与背压隔离。
- 客户端以 message_id 去重、以 room_seq 恢复。

### 3.3 业务价值怎么衡量

当前没有真实商业用户，不能声称留存、GMV、付费或人工成本收益。可验证的是工程指标：

- 建连成功率、峰值并发连接数、建连速率。
- 弹幕输入 QPS。
- 广播投递率 = 输入 QPS × 在线连接数。
- 端到端 P50/P95/P99 延迟。
- 投递缺口、重复率和 resync 成功率。
- Kafka 两个消费组 Lag。
- WebSocket 队列深度、丢旧数量、慢客户端断开数。
- Redis 补拉命中率和 MySQL 回源比例。

【待本人补证】个人贡献边界、项目投入时长、是否独立完成、每轮改造由谁提出和实施，必须由本人补充。

## 4. 项目范围与非目标

### 4.1 已实现

- 用户注册、登录、登出、查询当前用户。
- Redis Session 与 Cookie 鉴权。
- 房间创建、本人房间查询、列表、详情、修改、开播、关播。
- WebSocket 建连、Ping/Pong、读写协程。
- 游客只读、登录用户发送。
- Kafka Producer 和实时/持久化两个 Consumer Group。
- MySQL 弹幕历史与两套游标分页。
- Redis Pub/Sub、最近弹幕 ZSet、点赞计数。
- Hub 房间路由、16 分片、worker pool。
- 在线人数合并广播、点赞状态合并。
- 慢客户端背压、resync 补拉、热房采样与客户端轮询。
- pprof、WebSocket 指标、Kafka 实时链路指标。
- WebSocket/Kafka 压测器和自动化压测脚本。

### 4.2 没有实现

- RTMP/WebRTC 推流、转码、录制、CDN 和真实 HLS 媒体服务；当前 PlayURL 是固定示例域名拼接。
- 弹幕审核、敏感词、黑名单、禁言、风控和反刷。
- 完整 API 限流、单连接读大小限制、消息长度与类型白名单。
- 多 Broker、多副本、Redis Cluster、MySQL 高可用。
- 服务发现、Kubernetes、灰度发布、Prometheus/Grafana 告警。
- 全局在线人数、跨节点一致的热房判断和全局房间状态。
- 点赞持久化和严格幂等。

## 5. 架构：四层业务面 + 事件驱动实时面

### 5.1 总图

~~~mermaid
flowchart LR
    U[浏览器/压测客户端]
    H[HTTP Handler]
    S[Service]
    R[Repo]
    D[DAO]
    M[(MySQL)]
    RS[(Redis Session)]
    W[WebSocket Client]
    KP[Kafka Producer]
    K[(Kafka danmu_topic)]
    RC[Realtime Consumer Group]
    PC[Persistence Consumer Group]
    Z[(Redis Recent ZSet)]
    P[(Redis Pub/Sub)]
    SUB[Redis Subscriber]
    HUB[WebSocket Hub]

    U -->|HTTP| H --> S --> R --> D
    D --> M
    D --> RS
    U <-->|WebSocket| W --> KP --> K
    K --> RC --> Z
    RC --> P --> SUB --> HUB --> W
    K --> PC --> M
    U -->|replay after_seq| H
    H --> S
    S -->|优先| Z
    S -->|窗口未覆盖| M
~~~

### 5.2 HTTP 四层

| 层 | 目录 | 职责 | 不应承担 |
|---|---|---|---|
| Handler | internal/handler | 参数绑定、HTTP 状态码、响应格式 | SQL、复杂业务规则 |
| Service | internal/service | 权限、状态机、限额、流程编排 | 具体数据库实现 |
| Repo | internal/repo | 用户、房间、弹幕的查询和持久化语义 | HTTP 协议 |
| DAO | internal/dao | MySQL/Redis 客户端、Lua、迁移、基础操作 | 房主权限等业务判断 |

这四层在用户和房间接口中基本成立。需诚实说明：Handler 仍有状态码不准确等问题，Repo 与 DAO 的边界也不完全统一，例如部分 Repo 直接使用全局 GORM DB。

### 5.3 实时数据面

实时面不是简单套四层：

- internal/ws/handler.go：握手、可选 Session、房间状态校验。
- internal/ws/client.go：每个连接的读写协程、队列、ACK、Kafka 生产。
- internal/kafka：可靠交接、实时消费、持久化消费。
- internal/dao/recent_danma.go：最近窗口与发布原子化。
- internal/ws/redis_subscriber.go：跨实例消息进入本机。
- internal/ws/hub.go：房间路由、分片、广播和背压。
- web/danmaku-client.js：重连、去重、补拉和热房轮询。

其边界原则是“接入、交接、分发、扇出、恢复”各有唯一责任，不让 MySQL 或慢连接进入实时主路径。

## 6. 目录结构内化

~~~text
live-stream/
├─ main.go                         依赖初始化、协程启动、诊断端口、优雅退出入口
├─ config/config.yaml              MySQL/Redis/Kafka/热房/慢客户端参数
├─ internal/
│  ├─ auth/                        Session token 生成、哈希与 Redis 存取
│  ├─ config/                      Viper 配置加载与默认值
│  ├─ middleware/                  Cookie Auth 与 CORS
│  ├─ rousters/                    Gin 路由注册（目录名当前有拼写问题）
│  ├─ handler/                     HTTP Handler
│  ├─ service/                     用户、房间、历史/补拉业务逻辑
│  ├─ repo/                        MySQL 查询、弹幕幂等持久化
│  ├─ dao/                         DB/Redis 初始化、迁移、Lua、基础命令
│  ├─ model/                       配置、表模型、请求和响应 DTO
│  ├─ kafka/                       Producer、两个 Consumer、指标
│  └─ ws/                          握手、Client、Hub、热房策略、指标
├─ web/danmaku-client.js           推优先、拉补偿的参考客户端
├─ cmd/benchmark/                  WebSocket 与 Kafka 压测程序
├─ scripts/load-test.ps1           端到端自动压测
└─ docs/                           协议、压测、变更与项目复盘
~~~

## 7. 接口、数据表和中间件对象

### 7.1 HTTP/WebSocket 接口

| 方法 | 路径 | 鉴权 | 业务作用 |
|---|---|---|---|
| POST | /api/v1/auth/register | 否 | 创建用户 |
| POST | /api/v1/auth/login | 否 | 校验密码并创建 Redis Session |
| GET | /api/v1/auth/me | 是 | 查询当前登录用户 |
| POST | /api/v1/auth/logout | 是 | 删除 Redis Session 和 Cookie |
| GET | /api/v1/rooms | 否 | 按点赞降序返回开播房间 |
| GET | /api/v1/rooms/mine | 是 | 查询当前登录用户拥有的房间 |
| GET | /api/v1/rooms/:room_id | 否 | 房间详情 |
| POST | /api/v1/rooms | 是 | 每个用户创建一个房间 |
| PATCH | /api/v1/rooms/:room_id | 是且房主 | 修改标题 |
| POST | /api/v1/rooms/:room_id/start | 是且房主 | 开播 |
| POST | /api/v1/rooms/:room_id/stop | 是且房主 | 关播 |
| GET | /api/v1/rooms/:room_id/messages | 否 | 按 MySQL 主键向过去翻页 |
| GET | /api/v1/rooms/:room_id/messages/replay | 否 | 按 room_seq 向前补拉 |
| GET Upgrade | /api/v1/ws?room_id= | 可匿名观看 | 建立房间 WebSocket |

### 7.2 MySQL 表

users：

- user_id：主键、自增。
- user_name：唯一索引。
- user_email：唯一索引。
- password_hash：Argon2id 编码串，不序列化到 JSON。
- avatar_url、created_at、updated_at。

rooms：

- room_id：主键、自增。
- room_title、status、owner_id、start_at、like_count。
- push_url 不返回前端，play_url 当前由查询代码拼接。
- 当前 owner_id 没有数据库唯一约束，无法真正保证并发下“一人一房”。

danmas：

- id：主键、自增。
- message_id：唯一索引，消费幂等键。
- room_id、room_seq：组合唯一索引 idx_danmas_room_seq。
- user_id、user_name、content、created_at。
- 历史索引 idx_room_history 当前是 room_id + created_at，但旧历史接口按 room_id + id 查询，仍有索引优化空间。

### 7.3 Redis 对象

| Key/Channel | 类型 | 用途 | TTL/边界 |
|---|---|---|---|
| session:{sha256(token)} | String | Session → user_id | 7 天 |
| room:recent:{room_id} | ZSet | 最近可恢复弹幕，score=room_seq | 默认 1000 条、300 秒 |
| room:like:{room_id} | String Counter | 点赞总数 | 当前无 TTL、无 MySQL 回写 |
| live:danmu:broadcast | Pub/Sub Channel | 跨实例实时广播 | 不持久化、无离线积压 |

### 7.4 Kafka

| 对象 | 当前值/策略 |
|---|---|
| Topic | danmu_topic |
| Message Key | room_id 的十进制字节 |
| Balancer | Hash |
| Producer | 同步 WriteMessages，RequireAll，BatchSize=100，BatchTimeout=5ms |
| 持久化 Group | live-danmu-consumer |
| 实时 Group | live-danmu-consumer-realtime |
| 实时拉取 | MaxWait=50ms，提交间隔/批次窗口 200ms |
| 持久化批次 | 100 条或约 200ms |

## 8. 核心端到端链路

### 8.1 注册与登录

1. Handler 使用 Gin binding 校验邮箱和密码长度。
2. 注册时使用 Argon2id 生成带随机盐的密码哈希。
3. MySQL 用户表保存哈希，不保存明文。
4. 登录查询邮箱并校验哈希。
5. crypto/rand 生成 32 字节随机 Token，Cookie 保存原 Token。
6. Redis 只保存 Token 的 SHA-256 哈希到 user_id 映射，TTL 7 天。
7. Auth 中间件从 Cookie 取 sid、查 Redis，把 user_id 写入 Gin Context。

解决的问题：服务端可主动注销 Session；数据库泄露不会直接暴露明文密码；Redis 适合高频会话读取。

当前边界：配置中提交了明文数据库密码；没有登录限流、CSRF Token、Session 续期与多设备管理。

### 8.2 创建和开播

1. Auth 提供可信 user_id。
2. Service 查询 owner_id 是否已有房间。
3. 创建默认 status=0。
4. 开播再次查询房间，校验房主和状态。
5. MySQL 更新 status=1、title 和 start_at。
6. WebSocket 握手只允许进入存在且已开播房间。

解决的问题：避免客户端伪造 owner_id；把房间状态机集中在 Service。

当前边界：先查后插没有 owner_id 唯一索引，两个并发请求可能创建两个房间；开关播也不是带旧状态条件的原子更新。

### 8.3 一条弹幕从发送到 accepted

1. readPump 从 WebSocket 读 JSON。
2. ACK 消息只推进连接内 lastAck，不进入 Kafka。
3. 匿名用户发送被拒绝。
4. message_id 为空则生成 UUID；有值则校验 UUID。
5. 服务端覆盖 user_id、username 和 room_id。
6. 默认 type=danma。
7. Kafka Producer 以 room_id 为 Key 同步写入。
8. Kafka 返回成功后，通过控制队列给发送者返回 accepted。

accepted 的准确语义是“Kafka Producer 调用成功并达到当前 RequiredAcks 条件”，不是“已写 MySQL”，更不是“所有观众已收到”。

### 8.4 实时广播链路

1. 实时 Consumer Group Fetch Kafka 消息。
2. 使用 Kafka partition offset+1 填入 room_seq。
3. danma 通过 Lua 完成 ZADD、裁剪、EXPIRE、PUBLISH。
4. Redis 成功后消息进入待提交 offset 批次。
5. 每个实例的 Redis Subscriber 收到 Pub/Sub。
6. Hub 根据 room_id 查房间，计算热房投递策略。
7. 同一条消息只 JSON 编码一次并创建一个 PreparedMessage。
8. 房间按 16 个客户端 shard 生成 fanout job。
9. 每个 room+shard 固定到同一个 worker，维护该客户端 FIFO。
10. worker 把 PreparedMessage 放入每个 Client 的有界事件队列。
11. writePump 设置 10 秒写超时并写 Socket。

### 8.5 持久化链路

1. 独立持久化 Group 最多收 100 条或等待约 200ms。
2. 反序列化并同样补充 room_seq。
3. 在一个 MySQL 事务内逐条处理 danma。
4. 先按 message_id 查询；已存在视为成功 no-op。
5. 不存在则插入 danmas。
6. 整批成功后提交所有 Kafka Message，使每个分区推进到各自最高 offset。
7. 失败则持续重试，不推进持久化 offset。

准确表述是“批量拉取 + 单事务逐条查询/插入”，不是“真正 bulk insert”。它降低事务提交次数，但每条仍可能有一次 Count 和一次 Insert。

### 8.6 缺口恢复

1. Client 事件队列满或热房高水位时，服务端丢最旧事件、保留最新事件。
2. 通过高优先级控制队列发送 resync_required。
3. after_seq 是该连接最后写入 Socket 的 room_seq，latest_seq 是触发缺口的最新序号。
4. 客户端暂停直接展示，把实时流暂存到 liveBuffer。
5. 请求 replay?after_seq=...。
6. Service 先读 Redis ZSet；若游标早于窗口最老记录，则回源 MySQL。
7. 按 room_seq 升序合并，message_id 去重。
8. 达到目标后再排序并释放缓冲的实时消息。

### 8.7 热房推拉结合

Hub 估算：

    fanout_qps = source_qps × 当前实例房间连接数

超过配置的 150,000 次投递/秒预算后：

- sample_stride = ceil(fanout_qps / budget)，最大 16。
- 服务端只推每 stride 条中的一条。
- 通过 delivery_policy 告诉客户端进入 sampled 模式。
- 客户端以约 1 秒周期加随机抖动，从最近消息接口补拉。
- 恢复到 push 模式时再补一次尾部。

它解决的是“优先保住最新体验和系统可用性”，不是减少源消息或保证零延迟全量到达。

### 8.8 点赞

1. 登录连接发 type=like。
2. 同样先写 Kafka。
3. 实时组在一个 MySQL 事务中插入 like_events(message_id) 并更新 rooms.like_count。
4. message_id 已存在时事务返回当前计数，不重复加一；因此 Kafka 重放是幂等的。
5. MySQL 成功后将权威计数镜像到 Redis，同一房间在约 200ms 内只保留最新 count。
6. 以 system/like_count 状态更新发布到 Pub/Sub。
7. Hub 与在线人数合并成 room_state，用容量 1 的状态队列覆盖旧值。
8. Redis 启动和运行期间定期从 MySQL 恢复；单用户单房间默认每秒最多点赞 10 次。

已修复的历史边界：

- message_id 通过 like_events 主键持久化去重，计数与事件记录处于同一事务。
- rooms.like_count 现在是权威计数，榜单与详情能读取最新持久化值。
- Redis 是可重建镜像，启动时兼容旧 Redis 计数，之后定期按 MySQL 对账恢复。

## 9. 核心技术细节

### 9.1 为什么是两个 Kafka Consumer Group

如果一个消费者先落 MySQL 再发 Redis，MySQL 抖动会直接增加直播延迟。两个 Group 各维护独立 offset：

- 实时组的弹幕路径只依赖 Redis；点赞为了持久幂等会先完成一个短 MySQL 事务。
- 持久化组只依赖 MySQL，目标是历史耐久。

代价是两条链路只有最终一致性，观众已看到但历史接口短时间查不到属于允许状态。

### 9.2 顺序如何保证

当前顺序链：

1. Producer Key=room_id。
2. Hash Balancer 让同一房间进入同一 Kafka 分区。
3. Kafka 保证单分区有序。
4. room_seq 使用该分区 offset+1。
5. Hub 的 room+shard 固定映射 FIFO worker。
6. 每个客户端只有一个 writePump。

边界：

- 同分区中其他房间会制造序号空洞，所以 room_seq 单调但不连续。
- Kafka 扩分区后同一 Key 可能重新映射，新的 partition offset 可能小于旧值，破坏 room_seq 单调和数据库组合唯一约束。
- 生产演进应使用独立房间序列号服务、数据库/Redis 原子序列，或把 cursor 设计为 partition+offset 而不是单 uint64。

### 9.3 幂等与至少一次

可能重复的窗口：

- Producer 返回错误但 Broker 实际已写入，客户端用同一 message_id 重试。
- Redis 发布成功但实时 offset 提交前进程崩溃。
- MySQL 事务成功但持久化 offset 提交前崩溃。

处理：

- MySQL message_id 唯一索引。
- 持久化消费者把重复视为成功。
- 客户端按 message_id 去重。
- Redis ZSet 同一 room_seq 的重放会覆盖同一 score。

不足：

- 客户端去重集合只有最近 10,000 个且只在内存。
- like INCR 不是幂等。
- Count 后 Insert 不是最优写法，可直接 INSERT ... ON DUPLICATE KEY 或批量 upsert。

### 9.4 Redis Lua 为什么必要

如果 ZADD、裁剪、EXPIRE、PUBLISH 分开执行，可能出现：

- 已发布但尚未缓存，客户端收到 resync 后立刻补不到。
- 已缓存但没 TTL，形成残留。
- 并发裁剪窗口不一致。

Lua 在 Redis 单线程执行上下文中把四步原子化，使“可恢复记录先存在，再对外发布”成为一个不可分割操作。

注意：Lua 只能保证 Redis 内部原子性，不能让 Kafka offset 提交与 Redis 操作形成分布式事务。

### 9.5 三类客户端队列

| 队列 | 容量 | 语义 |
|---|---:|---|
| send | 256 | 弹幕事件，正常 FIFO；过载可丢旧并补拉 |
| control | 8 | accepted、error、resync、delivery_policy，高优先级 |
| state | 1 | 在线人数、点赞总数，只保留最新状态 |

这种拆分避免“几百条旧弹幕堵住 resync 指令”，也避免在线人数每次变化都排队。

### 9.6 锁、Channel、Once 和 Atomic

- Hub.mu 只保护 rooms 字典。
- Room.mu 保护单房间 clients、shards、onlineUsers、likeCount 和策略状态。
- sendMu 统一保护 Client 三个发送 Channel 的投递与关闭，防止 send on closed channel。
- unregisterOnce 和 closeOnce 保证注销和 Socket 关闭幂等。
- Atomic 保存连接序号、ACK、队列压力、最后写出游标和指标。
- 广播时先复制客户端快照再释放 Room 锁，避免持锁执行网络相关路径。

### 9.7 慢客户端策略

当前策略是：

- 热房达到 75% 高水位后即丢最旧事件并保留最新。
- 非热房在队列满后也丢旧保新。
- 每 5 秒最多发一次 resync 提示。
- 连续积压 100 次或持续 5 秒才断开。
- control 队列满则直接关闭，因为客户端连恢复指令都收不到。

取舍：牺牲单个慢客户端的逐条实时完整性，保护整个房间的低延迟和内存上限，再用拉取补偿完整性。

## 10. 压测与容量

### 10.1 必须分清的指标

| 指标 | 含义 |
|---|---|
| 并发连接数 | 同时保持多少 WebSocket，不是 QPS |
| 建连速率 | 每秒完成多少握手 |
| 输入 QPS | 每秒进入 Kafka 的弹幕/点赞数 |
| 广播投递率 | 每秒向客户端执行多少次消息投递 |
| 端到端延迟 | 客户端发送时间到客户端收到广播的时间 |
| 最终缺口 | expected - received |
| Kafka Lag | Consumer 尚未处理的 Kafka 记录数，不等于客户端积压 |

### 10.2 历史证据

【历史实测】仓库旧复盘记录：

- 10,000 个 WebSocket 全量建连成功，Dial 失败为 0。
- 1,000 连接、10 个发送者、约 9,800 次广播投递/秒曾实现零缺口。
- 10,000 连接下，约 50k/60k/70k/100k 投递目标曾出现约 0.10%/0.24%/1.14%/8.95% 缺口。

【重要限制】后续代码加入 PreparedMessage、可靠补拉、热房采样和新的慢连接策略后，变更记录明确写明尚未重新完成全链路高并发测试。因此这些数字只能描述历史版本和问题演进，不能直接证明当前 HEAD 的新边界。

### 10.3 容量估算示例

假设：

- 单房间 10,000 连接。
- 10 条弹幕/秒。
- 单帧应用层负载约 1 KiB。

则：

    投递率 = 10 × 10,000 = 100,000 次/秒
    纯负载带宽 ≈ 100,000 × 1 KiB = 97.7 MiB/s

这还没有计算 WebSocket 帧头、TCP/IP、TLS、重传和系统调用开销。若所有用户每秒都发一条，则输入 10,000 QPS、目标投递 1 亿次/秒，单机架构显然不成立。

内存也应按“连接固定成本 + 队列引用 + 堆积帧 + Socket 缓冲”估算，而不能只看 Client 结构体。当前 PreparedMessage 在同一广播内共享，队列主要保存指针和 room_seq，明显优于每个连接复制完整 JSON；但队列 256 × 连接数仍会产生可观内存与 GC 扫描成本。

### 10.4 当前压测成功条件

自动脚本已经修复旧版误报，当前默认要求：

- go test 和 go vet 通过。
- HTTP、登录、Session、建房和开播成功。
- dial_attempted、dial_succeeded 和 peak_conns 等于目标。
- benchmark errors=0。
- received 不得大于 expected，避免重复被当作成功。
- 丢失率不超过 MaxLossPercent，默认 0。
- 可选要求 P95 不超过阈值。
- 两个 Kafka Group 最终 Lag 回落。

## 11. 故障排查方法

### 11.1 统一排查顺序

~~~text
先确认症状和时间窗
→ 明确是发送失败、广播延迟、客户端缺口、历史缺失还是榜单错误
→ 用 message_id / room_id / room_seq 贯穿链路
→ 看 Producer write、两个 Kafka Lag、Redis publish、Hub 队列、Socket 写
→ 再看 CPU/内存/GC/网卡/文件描述符
→ 最后做单变量隔离与故障注入
~~~

### 11.2 指标定位表

| 症状 | 先看 | 判断 |
|---|---|---|
| accepted 很慢 | kafka_producer_write_duration | Producer batching、Broker、网络 |
| accepted 正常，所有观众都慢 | kafka_realtime_message_age、redis_publish_duration | 实时消费或 Redis |
| 只有部分观众慢/丢 | client_queue_depth、queue_full、write_errors | 慢客户端或扇出 |
| 观众看到但历史没有 | persistence Group Lag、MySQL 慢查询 | 持久化链路 |
| Kafka Lag=0 但客户端缺 | ws 队列与压测机资源 | Kafka 完成不代表 Socket 完成 |
| Redis 补拉大量回 MySQL | 最近窗口命中率、TTL、persistence Lag | 窗口太小或持久化太慢 |
| 榜单点赞不变 | Redis like 与 rooms.like_count | 当前实现未回写 MySQL |

### 11.3 已发生过的典型问题

- 在线人数风暴：每次 join/leave 全房广播，集中建连形成近似 O(N²)；改为 dirty room + 定时合并。
- send on closed channel：广播与注销并发；改为 sendMu、sendClosed、Once。
- Kafka 尾批次不刷新：阻塞 FetchMessage 让 ticker 无法执行；改为独立 fetch goroutine。
- 只提交最后一个分区 offset：改为把批次所有 Kafka Message 传给 CommitMessages。
- 假健康 Producer：只创建 Writer 没有真正连 Broker；启动时增加 Leader 探测。
- 秒级固定延迟：kafka-go 默认 BatchTimeout 为 1 秒；显式改为 5ms，并加回归测试。
- Docker 网段冲突：容器桥接网段与客户端局域网重叠，回包走错路由；Compose 固定到 10.250.0.0/24。
- 历史表主键错误：AutoMigrate 无法修复旧表已有结构；通过数据感知迁移和运行时 schema 验证处理。

## 12. 二三轮高频场景判断

### 12.1 Redis 故障

当前行为：

- 登录鉴权失败。
- 实时 Consumer 在 publish/cache 上持续重试，不提交 offset，Kafka Lag 上升。
- 持久化 Group 仍可写 MySQL。
- Redis Subscriber 如果退出，main 只记录日志，不会自动重启订阅。

恢复后风险：

- 若 Consumer 仍存活，会从 Kafka 继续发布。
- 若 Subscriber 已退出，本实例不会再收广播，必须重启进程。
- Pub/Sub 在无订阅者时返回成功但消息无人接收，在线客户端可能静默缺消息；最近 ZSet 可补，但当前服务端无法从 partition offset 空洞可靠识别单房间缺口。

生产方案：

- 订阅循环带指数退避和重新订阅。
- readiness 反映 Redis Subscriber、两个 Consumer 的存活状态。
- 通过可连续的房间序号或服务端定期 watermark 发现缺口。
- 重要实时消息可考虑 Redis Streams/Kafka 直推网关，而不是仅 Pub/Sub。

### 12.2 Kafka 故障

当前行为：

- 新进程启动时 Leader 探测失败会退出。
- 运行中 WriteMessages 使用 Hub 根 Context，没有单消息超时，readPump 可能长时间阻塞。
- 客户端只有在 WriteMessages 返回错误后才收到 503 和“同 message_id 重试”提示。
- 已在 Kafka 中的消息仍可被消费者处理，取决于 Broker 可用性。

生产方案：

- 每次生产使用短超时 Context。
- 熔断、限流和快速失败，防止 goroutine 堆积。
- 多 Broker、多副本、min.insync.replicas 与 acks=all 配套。
- 生产失败率和耗时告警，必要时本地短暂缓冲但要明确数据丢失边界。

### 12.3 MySQL 故障

当前行为：

- 启动连接失败直接 panic。
- 运行中 HTTP 用户/房间接口失败。
- 持久化 Group 无限重试当前批次且不提交 offset。
- 实时 Group 不依赖 MySQL，已有 WebSocket 仍可继续看弹幕。

风险：

- 一个不可写的毒消息会让整个持久化消费循环停在同一批次。
- Redis 最近窗口过期后，补拉可能回源到仍落后的 MySQL。

生产方案：

- 重试上限 + DLQ/隔离表 + 告警。
- 批次拆分定位毒消息。
- 连接池、超时、慢查询和事务耗时指标。
- replay 响应返回数据水位，让客户端知道持久化尚未追平。

### 12.4 缓存击穿、穿透和雪崩

本项目的核心风险不是传统“查不存在商品”：

- 穿透：任意 room_id 的 replay 会查 Redis 再查 MySQL；应先校验房间存在、对空结果做短 TTL 缓存和限流。
- 击穿/热点：热房成千上万客户端每秒轮询同一个 ZSet；即使单次查询快，也会形成热点 Key 和响应带宽压力。
- 雪崩：大量 room:recent Key 的 TTL 都是固定 300 秒，若同时创建可能集中失效并回源 MySQL。

方案：

- TTL 加随机抖动。
- 服务器按 room_id 合并相同 after_seq 的请求或做 singleflight。
- 节点内共享 replay 结果，热房由房间级拉取器统一读取后再分发。
- 请求限流、分页上限和热点 Key 分片。
- 对 sampled 模式按客户端分组错峰；当前客户端已有 0.8～1.2 倍抖动，但仍不足以解决大规模热点。

### 12.5 幂等

弹幕：

- message_id 由客户端生成或服务端补充。
- Kafka 重试、消费重放都保留 message_id。
- MySQL 唯一键和客户端 Set 去重。

点赞：

- 当前虽然也有 message_id，但 Redis INCR 没有幂等记录。
- 应使用 Lua：若 SETNX like:event:{message_id} 成功才 INCR，并设置去重窗口 TTL；或者先写 durable like_events，再异步聚合。

### 12.6 推拉结合

推适合低延迟，拉适合补偿和控制服务端扇出。合理策略不是固定二选一，而是：

- 普通房间全推。
- 单连接局部积压时，只让该连接补拉。
- 热房全局超预算时采样推 + 错峰拉。
- 极端热点可进一步只推 watermark/摘要，由边缘节点或客户端批量拉。

必须防止“WebSocket 省下的扇出压力全部变成 Redis/HTTP 轮询风暴”。

## 13. 当前源码中的优先风险

### P0：正确性与可用性

1. 消息缺少类型白名单、content 长度/空值校验和 WebSocket ReadLimit；超大或非法消息可能先实时广播，再让 MySQL 持久化永久重试。
2. Redis Subscriber 和后台 Consumer 退出后没有 supervisor 自愈，只写日志。
3. 点赞不幂等、不持久化，与房间榜单数据源不一致。
4. room_seq 绑定 partition offset，Kafka 扩分区会破坏单调性。
5. 一人一房只靠先查后插，缺少 owner_id 唯一索引。
6. 优雅退出没有等待 Hub、Subscriber 和两个 Consumer 完成；HTTP Shutdown 也不会自动关闭已 Hijack 的 WebSocket。
7. 配置和 Compose 中含明文凭据，不应进入生产仓库。

### P1：性能与可观测性

1. persistence 是逐条 Count+Insert，不是真正批量 upsert。
2. Redis replay 热房轮询可能形成热点 Key。
3. Redis Pub/Sub 的无订阅者窗口可能静默丢实时通知。
4. 房间榜单 WHERE status ORDER BY like_count 缺组合索引。
5. 旧历史查询 room_id + id 与现有 room_id + created_at 索引不匹配。
6. 每个 WebSocket 握手都会查房间；带 Session 的连接还会查 Redis 和 MySQL 用户，可增加缓存或握手令牌。
7. 指标当前是 JSON 内存计数和日志，不是 Prometheus 指标，也没有告警。

### P2：代码质量和协议

1. rousters、UserReigister、ErrInvalidEamil 等命名拼写需修正。
2. GetRoomDetail 忽略 room_id 解析错误，部分错误状态码使用 500。
3. StartRoomRequest 没有 required/min/max 校验。
4. onlineUsers 维护了账号计数，但广播使用 len(clients)，名称“在线人数”实际是连接数。
5. ACK 目前只记录，不驱动服务端重传或端到端成功判定。

## 14. 排序策略与工程升维

当前房间列表只取 status=1，并按 MySQL like_count 降序。当前边界是：

- 点赞已经实时持久化并同步 Redis，但单房间高频点赞会形成 MySQL 热点行，后续可按房间分桶聚合。
- 单一累计点赞让老房间长期占优。
- 没有在线人数、互动速率、新鲜度和风控降权。

可演进为：

    score = w1 × log(1 + recent_likes)
          + w2 × log(1 + online_users)
          + w3 × danmaku_rate
          + w4 × freshness_decay
          - risk_penalty

工程实现可分层：

- Redis ZSet 保存分钟级实时热榜。
- Kafka/Flink 或定时任务聚合窗口指标。
- MySQL 保存可追溯快照和房间基础数据。
- API 从 Redis 读榜单，MySQL 补详情。
- 明确榜单允许秒级最终一致，而房主权限必须强一致。

## 15. 从零复刻一个可运行版本

### 15.1 最小版本

1. 建 users、rooms、danmas 三张表和模型。
2. 实现注册、登录、Redis Session 和 Auth。
3. 实现房间 CRUD、房主权限和状态机。
4. 实现 WebSocket Handler、Client readPump/writePump、Hub room map。
5. 先做单进程内存广播，加入 Ping/Pong、写超时和有界队列。
6. 引入 Kafka Producer，room_id Key + Hash。
7. 加持久化 Consumer，message_id 唯一键。
8. 加实时 Consumer → Redis Pub/Sub → Hub，移除 MySQL 对实时链路的阻塞。

### 15.2 可靠版本

9. 引入 room_seq、最近 ZSet、Lua 原子缓存发布。
10. 实现 replay 接口、客户端重连、去重与 liveBuffer。
11. 分离 send/control/state 队列。
12. 实现慢客户端 resync 和持续过载断开。
13. 实现房间分片 worker 与 PreparedMessage。
14. 实现热房预算、采样和推拉切换。
15. 增加 pprof、链路指标、严格压测和故障注入。

### 15.3 生产化版本

16. 改造稳定房间序列，消除扩分区风险。
17. 后台任务 supervisor、readiness、告警和优雅退出等待。
18. 限流、消息校验、审核、禁言和风控。
19. Kafka/Redis/MySQL 高可用与凭据管理。
20. 点赞幂等聚合和热榜数据一致性。

## 16. 测试核验结果

2026-09-02 在当前工作区执行：

- go test ./...：通过。
- go vet ./...：通过。
- go test -race ./...：未执行成功，当前 Windows Go 环境未启用 CGO，race detector 要求 CGO。
- 覆盖率：config 约 86.1%，middleware 约 47.2%，kafka 约 9.3%，ws 约 55.9%；auth/dao/handler/repo/service 基本为 0%。

这说明核心 Hub 并发行为有较多回归测试，但登录、权限、数据库事务、幂等落库和端到端链路仍缺自动化测试。测试通过不能替代真实 MySQL/Redis/Kafka 集成测试，也不能证明旧数据库 schema 正确。

## 17. 复盘口径

### 17.1 可以主动讲的亮点

- 用 Kafka 双消费组把低延迟广播和 MySQL 耐久解耦。
- 用 room_id 分区和固定 shard worker 把 Kafka 分区顺序延续到单客户端。
- 用三类队列表达事件、控制和可覆盖状态的不同语义。
- 用 ZSet + Lua + replay + message_id 实现可补偿的至少一次链路。
- 从 O(N²) 在线状态广播、channel close race 和 Kafka 默认 batching 延迟等真实问题出发迭代。
- 用严格的 expected/received、P95 和 Lag，而不是模糊的“高并发”做性能判断。

### 17.2 必须主动承认的边界

- 历史 10,000 连接数据不是当前最新架构的完整复测结果。
- Kafka/Redis/MySQL 都是本地开发级单点配置。
- Pub/Sub 不是可靠队列；完整性来自 Kafka、最近缓存、历史库和客户端恢复的组合，但仍有静默缺口。
- room_seq 方案不支持无痛扩 Kafka 分区。
- 点赞链路和热榜尚未闭环。
- 项目没有真实业务指标，只能讲工程指标。

### 17.3 如果重来一次

1. 先定义 SLO：accepted P95、端到端 P95、缺口率、恢复时间、房间规模。
2. 先设计稳定 room_seq 和消息协议，再选 Kafka 分区。
3. 一开始就做输入校验、限流和毒消息隔离。
4. 把后台任务纳入统一生命周期和健康检查。
5. 先搭可重复集成环境与压测判定，再做性能优化。
6. 点赞从第一天就按事件幂等和聚合一致性设计。

## 18. 待本人补证

以下内容不能从源码得出，面试前必须填实：

- 项目是独立完成还是协作完成，各自边界是什么。
- 最初为什么选择直播弹幕，目标岗位或学习目标是什么。
- 哪个问题最难、本人如何定位、做过哪些错误判断。
- 历史压测所用服务器 CPU、内存、网卡、系统参数和压测机配置。
- 最新架构部署后的新基线数据。
- 项目投入时间、关键里程碑和未来是否继续演进。

## 19. 源码导航

- 启动与生命周期：main.go
- 路由：internal/rousters/rousters.go
- HTTP：internal/handler/handler.go
- 业务规则：internal/service
- 数据访问：internal/repo、internal/dao
- 模型与表：internal/model
- Session：internal/auth/session.go、internal/middleware/auth.go
- Producer：internal/kafka/producer.go
- 持久化 Consumer：internal/kafka/consumer.go
- 实时 Consumer：internal/kafka/realtime_consumer.go
- WebSocket 接入：internal/ws/handler.go
- 单连接状态机：internal/ws/client.go
- 房间 Hub：internal/ws/hub.go
- 热房策略：internal/ws/adaptive.go
- 最近缓存：internal/dao/recent_danma.go
- 参考客户端：web/danmaku-client.js
- 压测：cmd/benchmark、scripts/load-test.ps1
- 可靠协议：docs/reliable-danmaku-protocol.md
- 历史压测复盘：docs/autumn-recruitment-project-review.md
- 变更证据：docs/automated-load-test-change-log.md
