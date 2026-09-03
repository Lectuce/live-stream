# [项目深挖] live-stream - 2026-09-03

> 文档定位：基于 2026-09-03 当前仓库源码、测试和已有压测记录形成的项目复盘。本文不仅记录“用了什么”，还解释每项核心设计为什么存在、解决什么业务问题、付出什么代价，以及如何验证。
>
> 证据标签：
>
> - 【源码事实】：当前代码可以直接证明。
> - 【历史实测】：仓库文档记录过，但最新架构改造后尚未重新完成同等规模全链路复测。
> - 【设计建议】：当前尚未实现，是面试时应主动承认的演进方向。
> - 【待本人补证】：源码无法证明，不能替本人虚构。

## 1. 先给结论

live-stream 是一个 Go 直播互动后端，完成了用户 Session、直播房间、WebSocket 弹幕与连续点赞、Kafka 可靠交接、Redis 实时分发与最近消息补偿、MySQL 历史持久化、累计点赞聚合，以及慢客户端治理、热房自适应推拉、实时房间热度榜和端到端压测。

它最有价值的地方不是“用了很多中间件”，而是围绕实时系统的四个矛盾做了工程化处理：

1. 实时性与可靠性：实时展示不能等待 MySQL，但消息又要能落历史。
2. 完整性与可用性：慢客户端不应拖垮整个房间，但丢弃后必须可补偿。
3. 单房间扇出与资源上限：输入 QPS 很低时，乘以在线人数后仍可能形成巨大的投递量。
4. 历史互动与当下活跃：累计点赞需要可靠、单调地展示，但不能直接决定实时排序，否则老房间会长期占榜、新房和突发活跃房间难以被发现。

当前项目适合定位为“具有可靠消息、背压、实时排序和容量验证意识的直播互动后端原型”，不能定位为完整直播平台或生产级高可用系统。它没有真实音视频推流链路、内容审核、通用接口限流、跨地域容灾、完整监控告警和真实业务用户；当前只有点赞入口具备明确的频率限制与热度贡献上限。

### 1.1 完成度判断

| 维度 | 当前判断 | 证据或边界 |
|---|---|---|
| 功能闭环 | 较完整 | 注册登录、房间管理、开关播、实时弹幕、历史与补拉均有接口 |
| 四层架构 | HTTP 面清晰 | Handler → Service → Repo → DAO；实时面是独立事件管道 |
| 实时链路 | 较深入 | Kafka 双消费组、Redis Pub/Sub、房间分片扇出、PreparedMessage |
| 可靠性 | 有设计但非绝对可靠 | 至少一次、消息去重、补拉；仍有 Pub/Sub 静默缺口和退出恢复问题 |
| 性能工程 | 有证据链 | 自研 benchmark、严格丢失率判定、延迟分位、pprof 与运行指标 |
| 测试 | 核心并发模块较好，业务层偏弱 | ws 覆盖约 51.1%；Repo 18.2%，热度、点赞批次和排序已有单测；Service/Handler 仍基本无自动测试 |
| 生产化 | 未完成 | 单 Kafka Broker、明文配置、缺少通用限流/审核、无服务自愈、无部署编排 |
| 业务价值 | 尚无商业数据 | 只能讲工程价值，不能虚构 DAU、收入或降本比例 |

## 2. 面试开场

### 2.1 30 秒版本

我做了一个基于 Go 的直播房间和实时互动后端。弹幕和点赞经 WebSocket 接入，以 room_id 作为 Kafka Key 保证房间内有序；两个 Consumer Group 分离实时广播和历史持久化。针对慢客户端与热房，我实现了有界队列、丢旧保新、resync 补拉和按扇出预算自适应采样。点赞采用“100 条或 200ms”聚合、MySQL checkpoint 幂等和 Redis 状态镜像；房间发现则把历史 like_count 与实时 hot_score 解耦，用近期点赞、弹幕、跨节点在线连接和开播衰减计算 Redis ZSet 榜单，故障时按开播时间降级。

### 2.2 3 分钟完整工程故事

这个项目起点不是单纯做房间 CRUD，而是想把直播弹幕从“WebSocket 收到后遍历连接发送”的演示，推进到一个能讨论可靠性、顺序、背压和容量边界的工程原型。

第一阶段我完成用户、房间和会话闭环。密码使用 Argon2id，服务端生成随机 Session Token，只把 Token 哈希存入 Redis；房间创建、修改、开播和关播都从 Session 获取用户身份并校验房主权限。HTTP 部分采用 Handler、Service、Repo、DAO 四层，分别负责协议、业务规则、查询语义和基础设施。

第二阶段是弹幕主链路。登录用户通过 WebSocket 发消息，服务端覆盖客户端伪造的 room_id、user_id 和 username，并生成或校验 message_id。Producer 使用 room_id 作为 Key，通过 Hash Balancer 写入 Kafka，RequiredAcks 为 RequireAll。Kafka 是可靠交接点，发送成功后只返回 accepted，不代表观众已收到或 MySQL 已落库。

Kafka 后面拆成两个 Consumer Group：实时组先把弹幕通过 Lua 原子写入 Redis ZSet、裁剪、续 TTL，再发布 Pub/Sub，成功后才推进实时组 offset；持久化组把弹幕事务写入 MySQL，成功后再提交自己的 offset。这样 MySQL 抖动只增加持久化 Lag，不阻塞直播间实时展示。重复消费通过 message_id 唯一索引和客户端去重承受。

第三阶段是扇出和过载治理。Hub 按 room_id 管理连接，一个房间再按连接 ID 分成 16 个 shard，每个 room+shard 固定映射到 FIFO worker；这样不同 shard 可以并行，同一客户端的消息仍不乱序。同一条广播只序列化一次并复用 PreparedMessage。每个客户端有事件、控制和可覆盖状态三类队列：在线人数和点赞总数只保留最新值，控制消息不被旧弹幕阻塞；慢客户端队列高水位后丢旧保新并发 resync_required，持续积压才断开。

第四阶段是推拉结合。每条弹幕有 room_seq，当前实现直接使用 Kafka partition offset+1。客户端断线、收到 resync 或进入热房采样模式后，用 after_seq 从 Redis 最近窗口补拉，窗口覆盖不到再回源 MySQL，并按 message_id 去重。热房根据源 QPS×本机连接数估算扇出，超过预算时只推部分消息，其余由客户端错峰轮询补齐。

第五阶段是点赞与房间发现。连续点赞不能一点击就开一个 MySQL 事务，因此实时消费者以 100 条或 200ms 为窗口，按房间聚合成 delta；事务内写 like_batches、原子增加 rooms.like_count，并推进 Kafka 分区 checkpoint，使重放不会重复累计。随后我把“历史累计点赞”和“当下是否值得推荐”拆开：like_count 继续可靠展示，hot_score 则用 Redis 近期互动、全局在线连接和新鲜度秒级重算。这样老房间不会仅凭历史资产长期霸榜，当前活跃和新开播房间有机会被发现，同时 Redis 故障不会破坏累计点赞真相。

最后我没有只报“几万 QPS”，而是把连接数、输入消息率、广播投递率、延迟和缺口分开。历史测试证明过 10,000 连接可全量建连，旧版本在 1,000 连接、约 9,800 次投递/秒下无损；更高扇出出现过不同程度缺口。最新推拉、自适应、点赞聚合和热榜版本尚未完成相同环境全链路复测，因此面试时我会把历史数据、当前代码能力与待验证业务价值分开讲。

## 3. 为什么做：业务问题与技术动作

### 3.1 直接原因

【源码与历史文档事实】项目要解决直播互动中最基本的闭环：

- 用户能注册、登录并维持身份。
- 主播能创建房间、修改标题、开播和关播。
- 游客能观看，登录用户能发送弹幕或点赞。
- 弹幕既要尽快出现在观众端，又要可持久化、可翻页、可断线恢复。
- 热门房间、集中建连和慢客户端不能把服务拖垮。
- 房间列表要反映“此刻值得进入”，而不是简单奖励历史累计点赞最多的房间。

### 3.2 根本原因

直播互动不是普通 CRUD。它具有长连接、一对多放大、瞬时热点、部分用户网络慢和跨实例广播等特征。如果沿用“请求到来 → 写 MySQL → 遍历所有连接同步发送”的直线方案，会把数据库延迟、单个慢连接和房间规模全部耦合到用户体验上。

房间排序也不是简单的 `ORDER BY like_count DESC`：累计点赞是跨整个生命周期不断增长的存量指标，实时热度是快速变化的流量指标。把两者共用一个字段，会让“用于展示的历史认可”意外成为“决定曝光的排序权力”，造成老房锁榜、新房冷启动困难、短期活跃无法反映以及刷赞直接影响流量分配。因此必须拆成可靠程度和更新频率不同的两条数据链路。

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
- 点赞聚合批次大小、事务次数、单房间热点行更新次数和 Kafka 重放后的重复累计数。
- 热榜刷新耗时、榜单读取 P95、Redis 降级次数、榜单抖动率和新开播房间曝光占比。
- 若进入真实业务验证，核心效果指标应是榜单进房点击率、进入后 30 秒停留率和互动转化率；护栏指标是头部曝光集中度、刷量占比和接口延迟。

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
- Redis Pub/Sub、最近弹幕 ZSet、点赞状态镜像、互动分钟桶和 `rooms:hot` 热榜。
- Hub 房间路由、16 分片、worker pool。
- 在线人数合并广播、点赞状态合并。
- 点赞 100 条/200ms 聚合、`like_batches` 与分区 checkpoint 重放幂等。
- 各网关在线连接心跳汇总、近期互动统计、热度计算和榜单降级读取。
- 慢客户端背压、resync 补拉、热房采样与客户端轮询。
- pprof、WebSocket 指标、Kafka 实时链路指标。
- WebSocket/Kafka 压测器和自动化压测脚本。

### 4.2 没有实现

- RTMP/WebRTC 推流、转码、录制、CDN 和真实 HLS 媒体服务；当前 PlayURL 是固定示例域名拼接。
- 弹幕审核、敏感词、黑名单、禁言、风控和反刷。
- 完整 API 限流、单连接读大小限制、消息长度与类型白名单。
- 多 Broker、多副本、Redis Cluster、MySQL 高可用。
- 服务发现、Kubernetes、灰度发布、Prometheus/Grafana 告警。
- 严格的全局独立用户数；当前热榜汇总的是各网关连接数，同账号多端和匿名连接不等于真实 UV。
- 跨节点统一的“推拉采样热房”判断；热度榜使用全局连接信号，但每个 Hub 的扇出降级仍按本机压力决策。
- 经过真实流量 A/B 实验校准的热度权重、风险惩罚和推荐效果评估。
- 点赞单次点击的永久审计；当前客户端重试去重采用 Redis TTL，MySQL 只保存聚合批次和消费 checkpoint。

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
    LB[(MySQL like_batches/checkpoint)]
    HM[(Redis 分钟桶/在线心跳)]
    HR[Hot Ranking Engine]
    HZ[(Redis rooms:hot)]

    U -->|HTTP| H --> S --> R --> D
    D --> M
    D --> RS
    U <-->|WebSocket| W --> KP --> K
    K --> RC --> Z
    RC --> P --> SUB --> HUB --> W
    K --> PC --> M
    K -->|点赞聚合| RC --> LB
    RC --> HM
    HUB -->|本机连接快照| HM
    M -->|开播房间/start_at| HR
    HM --> HR --> HZ
    H -->|榜单 ID| HZ
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

其边界原则是“接入、交接、分发、扇出、恢复、累计、排序”各有唯一责任。弹幕实时展示不等待 MySQL；点赞累计允许 200ms 级聚合以换取数据库效率；热榜允许秒级误差，以避免把频繁变化的推荐信号写成强一致业务状态。

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
│  ├─ kafka/                       Producer、两个 Consumer、点赞聚合、指标
│  ├─ hotrank/                     实时热度计算与周期刷新
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
| GET | /api/v1/rooms | 否 | 按 Redis 实时热度返回开播房间，异常时按开播时间降级 |
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
- `(status,start_at)` 组合索引用于 Redis 榜单不可用时查询最新开播房间，避免退回历史点赞排序。

danmas：

- id：主键、自增。
- message_id：唯一索引，消费幂等键。
- room_id、room_seq：组合唯一索引 idx_danmas_room_seq。
- user_id、user_name、content、created_at。
- 历史索引 idx_room_history 当前是 room_id + created_at，但旧历史接口按 room_id + id 查询，仍有索引优化空间。

like_batches：

- 每行代表一个 Consumer Group、Topic、Partition、房间和 offset 范围聚合出的 `delta`，不保存每次点击明细。
- `batch_id` 由消费组、分区、房间和 offset 范围生成，用于吸收完全相同聚合批次的重复插入。
- 选择保存聚合批次而不是逐点击事件，是为了减少表增长与房间热点行更新次数；代价是不能永久按 message_id 审计每次点击。

like_aggregation_checkpoints：

- 以 Consumer Group、Topic、Partition 唯一定位消费进度，保存 `last_offset`。
- checkpoint 与 `rooms.like_count += delta` 位于同一事务，解决“数据库已加数但 Kafka offset 尚未提交，重放后批次边界又变化”的重复累计问题。

### 7.3 Redis 对象

| Key/Channel | 类型 | 用途 | TTL/边界 |
|---|---|---|---|
| session:{sha256(token)} | String | Session → user_id | 7 天 |
| room:recent:{room_id} | ZSet | 最近可恢复弹幕，score=room_seq | 默认 1000 条、300 秒 |
| room:like:{room_id} | String | MySQL `like_count` 的实时镜像 | 无 TTL，可由 MySQL 启动/定期重建 |
| room:like:idempotency:{message_id} | String | 点赞 pending/accepted 入口幂等状态 | pending 60 秒，accepted 24 小时 |
| room:like:rate:{room_id}:{user_id}:{second} | Counter | 单用户单房间每秒点赞限流 | 2 秒 |
| room:hot:likes:1m:{room_id}:{minute} | Counter | 热榜近期点赞贡献 | 默认 180 秒 |
| room:hot:danmas:1m:{room_id}:{minute} | Counter | 热榜近期弹幕数 | 默认 180 秒 |
| room:hot:likes:user:1m:{room_id}:{user_id}:{minute} | Counter | 限制单用户对热榜的点赞贡献 | 默认 180 秒 |
| room:online:nodes:{room_id} | Hash | 每个网关的本机连接数 | 默认 10 秒 |
| room:online:heartbeats:{room_id} | ZSet | 网关心跳时间，用于剔除崩溃节点 | 默认 10 秒 |
| rooms:hot | ZSet | 实时房间榜，member=room_id、score=hot_score | 周期原子重建 |
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
| 点赞路径 | 与弹幕共用一个 Topic；实时组识别 type=like 后聚合持久化 |

为什么没有为了点赞再建一个 Topic：当前体量下，弹幕和点赞都属于房间互动事件，共用 `room_id` 分区键可以复用接入、顺序和运维链路。实时消费者在应用内按类型走不同处理：弹幕优先低延迟广播，点赞按 100 条/200ms 聚合。代价是点赞 MySQL 故障可能阻塞同一实时消费分区中更靠后的弹幕；当点赞吞吐、SLO 或故障隔离需求明显不同，拆独立 Topic 才更合理。

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
2. Redis Lua 原子预留 message_id 并执行单用户/单房间限流，然后同步写 Kafka。
3. 实时组累计 100 条或等待 200ms，按 Kafka 分区和房间聚合 delta。
4. MySQL 事务内写一条 like_batches、执行 rooms.like_count += delta，并推进分区 checkpoint。
5. checkpoint 跳过已处理 offset，因此 Kafka 重放即使形成了不同批次边界也不会重复计数。
6. MySQL 成功后将权威计数镜像到 Redis，再发布 system/like_count，最后提交 Kafka offset。
7. Hub 与在线人数合并成 room_state，用容量 1 的状态队列覆盖旧值。
8. Redis 启动和运行期间定期从 MySQL 恢复；单用户单房间默认每秒最多点赞 10 次。

为什么点赞也进入 Kafka，而不是先 `INCR Redis`：一旦服务端向用户返回 accepted，就需要有一个可重放的耐久交接点。若先改 Redis 再写 MySQL，进程崩溃、Redis 丢数据或双写失败时，很难判断一次点击是否已经被永久接收；Kafka 先确认可以把后续 MySQL 故障转化为消费 Lag。代价是一次点击不会立刻变成持久化总数，页面看到的是 100 条或 200ms 聚合后的最新状态。

为什么不为每次点击保存一行 `like_events`：直播点赞允许连续点击，单房间可能在短时间产生大量事件。逐条事务会造成 `rooms` 热点行竞争、事务次数和表增长同时放大。当前把一个消费窗口内同房间事件合成 `delta`，让 100 次点击从最多 100 次房间更新变成 1 次；如果未来需要风控取证或用户点赞明细，应把 Kafka 原始事件异步下沉到分析存储，而不是让在线计数表承担审计职责。

为什么 checkpoint 不能只靠 `batch_id`：进程在 MySQL 提交后、Kafka offset 提交前崩溃，重启后内存窗口可能把同一批 offset 切成不同边界，新的 batch_id 也随之变化。持久化 `last_offset` 才能独立于批次边界判断哪些原始 Kafka 事件已经计数。

已修复的历史边界：

- Kafka 重放通过 MySQL 分区 checkpoint 持久去重；客户端 message_id 重试通过 Redis TTL 去重。
- rooms.like_count 现在是权威累计值，列表卡片与房间详情都能展示最新持久化值，但它不再直接参与房间排序。
- Redis 是可重建镜像，启动时兼容旧 Redis 计数，之后定期按 MySQL 对账恢复。

### 8.9 “累计点赞 + 实时热度”为什么要解耦

#### 业务问题

`like_count` 回答的是“这个房间历史上累计得到多少互动”，适合做可见的社会证明；房间排序回答的是“用户现在进入哪个房间更可能获得活跃体验”，需要反映最近几十秒到几分钟的状态。若直接按历史点赞排序，会出现四个问题：

1. 老房间凭历史存量长期占据曝光，新开播房间几乎没有冷启动机会。
2. 一个历史点赞很高但当前无人互动的房间，仍可能排在正在爆发的房间前面。
3. 点赞既影响展示总数又直接决定流量分配，刷赞的业务收益过大。
4. 为了实时排序高频更新 MySQL，会把允许误差的推荐信号变成数据库热点和强一致负担。

因此拆成两种语义：

| 指标 | 回答的问题 | 存储与一致性 | 失败时的业务选择 |
|---|---|---|---|
| `like_count` | 历史累计有多少有效点赞 | Kafka → MySQL，最终一致、checkpoint 防重放 | accepted 后应最终计入，不能因 Redis 丢失永久消失 |
| `hot_score` | 当前是否值得获得更多曝光 | Redis 时间桶 + ZSet，秒级重算、允许少量误差 | Redis 故障时按开播时间降级，不能拖垮房间列表 |

解耦解决的根本问题不是“换一种排序公式”，而是把**互动展示资产**和**实时流量分配信号**的生命周期、可靠性和写入成本分别建模：一个需要可追溯地单调累计，另一个需要快速衰减、可重建和可降级。

#### 具体实现

1. **近期点赞**：入口 Lua 先做每秒限流和 message_id 预留；Kafka 返回成功后，把 pending 转为 accepted，并在同一个 Lua 中累加用户/房间/分钟贡献。历史 like_count 记录所有未被入口限流的有效点击，热榜默认只允许同一用户每分钟贡献 10 次，降低连续点击对刷榜的放大。
2. **近期弹幕**：实时消费者把 `ZADD NX`、最近窗口裁剪、TTL、分钟桶 `INCR` 和 Pub/Sub 放进一个 Lua。`NX` 让网络重试或 Kafka 重放在事件仍处于最近窗口时不重复增加弹幕热度，同时不增加一次独立 Redis 网络往返。
3. **在线连接**：每个 Hub 快照本机 `len(room.clients)`；热榜任务用实例 ID 把各房间连接数写入 Hash，并把心跳时间写入 ZSet。聚合时先剔除超过 TTL 的实例再求和，正常退出主动删除，异常退出靠 TTL 淘汰。
4. **近一分钟窗口**：按自然分钟存桶，读取“当前桶 + 上一桶仍与最近 60 秒重叠的比例”。它不是严格逐秒滑窗，但只需固定数量 Key 和一次 MGET，适合当前个人项目的复杂度与成本。
5. **打分**：默认每 3 秒读取开播房间、全局连接数和互动桶，计算：

       hot_score = 3 × log(1 + online_connections)
                 + 2 × log(1 + recent_likes)
                 + 1 × log(1 + recent_danmas)
                 + 1 × exp(-live_minutes / 30)

6. **写榜**：Lua 在一次原子执行中 `DEL rooms:hot` 后写入全部 ZSet member，避免列表请求读到只更新了一半的新榜单。
7. **读榜**：API 先 `ZREVRANGE` 取 room_id 和 score，再用 MySQL `WHERE room_id IN (...)` 批量查详情，并按 Redis 返回顺序重组。榜单含已关播房间时跳过并用最新开播房间补齐；Redis 报错或空榜时走 `(status,start_at)` 索引降级。
8. **页面语义**：卡片继续展示 `like_count`，首页文字明确写“累计点赞”；`hot_score` 只负责顺序，避免把一个内部算法分数误导成用户可理解的点赞数量。

#### 为什么公式这样设计

- 在线连接权重最高：它最直接反映房间此刻是否有人，但统计的是连接数而非严格 UV，所以必须用 `log1p` 压缩。
- 近期点赞权重第二：点赞表达低成本正反馈，适合捕捉突然活跃，但容易连点，因此既做对数压缩又限制单用户贡献。
- 弹幕权重较低：弹幕体现更强互动，却可能被少数高频用户刷屏；当前尚未按独立发言用户去重，所以不应给过高权重。
- 新鲜度是小幅加分：给新开播房间冷启动机会，但指数衰减后不会长期压过真实互动。
- 不直接相加原始值：在线数可能比每分钟互动大几个数量级，原始相加会让小量级信号失去作用；`log1p` 既处理 0，又降低头部房间的边际收益。

这些权重只是工程初值。没有曝光、点击、停留和互动数据，就不能说“3:2:1:1 是最优”；真实上线应以旧排序或按开播时间排序为对照做 A/B 实验。

#### 解耦后的故障边界

- Redis 热榜丢失：累计点赞不受影响，房间列表暂时按开播时间排序，后续心跳和互动重新生成榜单。
- MySQL 暂时不可用：已 accepted 的点赞留在 Kafka 等待聚合；旧 `rooms:hot` 不会因为一次计算失败被清空，但列表详情查询本身仍依赖 MySQL。
- 某个网关崩溃：其在线连接在 10 秒 TTL 后从全局在线数剔除；这段时间允许轻微高估。
- 同一用户连续刷赞：历史展示值仍记录通过入口限流的点击，但超过每分钟热榜贡献上限的部分不再换取额外曝光。

#### 仍未解决

- 在线指标是连接数，不是独立用户数；同账号多端、机器人和匿名连接会产生偏差。
- 点赞贡献只有简单频控，没有设备、账号质量、IP、行为序列等完整风控。
- 多实例都会运行热榜任务并替换同一 ZSet，虽然输入大体相同，但没有 leader election；生产可改为单独调度任务或流计算作业。
- Lua 涉及多个相关 Key，当前面向单实例 Redis；迁移 Redis Cluster 前需用 hash tag 重新设计同槽 Key。
- 没有真实业务 A/B 数据，无法证明新榜单提高了点击率或停留率。

## 9. 核心技术细节

### 9.1 为什么是两个 Kafka Consumer Group

如果一个消费者先落 MySQL 再发 Redis，MySQL 抖动会直接增加直播延迟。两个 Group 各维护独立 offset：

- 实时组的弹幕路径只依赖 Redis；点赞按 100 条/200ms 聚合后完成一个短 MySQL 事务。
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
- Redis 最近弹幕使用 `ZADD NX`，同一事件仍在窗口内时重放不会重复缓存或增加热度桶。
- 点赞入口的 message_id pending/accepted 状态吸收普通客户端重试；消费重放由 MySQL 分区 checkpoint 吸收。

不足：

- 客户端去重集合只有最近 10,000 个且只在内存。
- 点赞 message_id 的入口幂等只有 24 小时 Redis 窗口，Redis 数据丢失或窗口外重试不能提供永久的“同一点击”去重。
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

### 9.8 核心设计决策账本：为什么这样设计

| 设计 | 直接原因/业务问题 | 为什么选择当前方案 | 代价与触发演进条件 |
|---|---|---|---|
| 服务端覆盖 room_id/user_id/user_name | 客户端输入不可信，伪造身份会破坏权限、审计和房间隔离 | 身份来自 Session，房间来自连接上下文，客户端只表达内容和类型 | 仍需消息类型白名单、长度限制和内容审核 |
| Kafka 作为 accepted 边界 | 实时入口与下游可能分别故障，需要明确“已接收”语义 | Broker 确认后事件可被重放，客户端可安全结束本次发送 | accepted 不等于观众收到或 MySQL 落库；单 Broker 仍是单点 |
| `room_id` Key + Hash | 同一房间弹幕必须有稳定顺序 | Kafka 分区内有序，按房间路由可把顺序延续到消费者 | 扩分区会重新映射；超级房间仍可能形成单分区热点 |
| 一个 Topic 承载弹幕和点赞 | 当前两类都属于房间互动，规模和运维复杂度有限 | 复用连接协议、Producer、分区键和消费基础设施 | 点赞持久化故障可能阻塞同分区弹幕；SLO 分化后拆 Topic |
| 两个 Consumer Group | 实时展示不能被长期历史写入拖慢 | 两组各消费全量事件，分别优化低延迟和耐久性 | 多一份消费与运维成本；两条路径只能最终一致 |
| 弹幕 100 条/200ms 单事务 | 每条提交事务吞吐低，纯大批次又增加低流量延迟 | 数量阈值保吞吐、时间阈值保尾延迟 | 当前仍逐条 Count+Insert；下一步批量 upsert |
| 点赞按房间聚合 delta | 连点会让事务数、热点行锁和明细表增长一起放大 | 一个窗口内每房间只写一条 batch、更新一次房间计数 | 不保留永久逐点击审计；分析需求出现时下沉原始流 |
| 点赞 checkpoint 与计数同事务 | batch 边界在重启后会变化，单靠 batch_id 不足以防重 | 持久 offset 决定原始事件是否已计数，与内存分批无关 | checkpoint 粒度是分区，毒消息或事务故障会阻塞后续 |
| MySQL 是 like_count 真相源 | Redis 可能重启、清空，不能承诺 accepted 点赞永久存在 | MySQL 保存累计值，Redis 只做实时镜像并可对账恢复 | Redis 更新存在短暂延迟，但不影响最终累计 |
| 累计点赞与 hot_score 解耦 | 历史存量不能公平代表当前活跃，也不应直接控制曝光 | MySQL 管可靠累计，Redis 管可衰减、可重建排序 | 排名有秒级误差，需要实验校准权重和反作弊 |
| 分钟桶近似滑窗 | 严格逐事件滑窗存储和计算成本更高 | 当前桶加上一桶重叠比例，固定 Key 数即可近似 60 秒 | 边界附近有近似误差；规模上升可用流处理窗口 |
| `log1p` 压缩热度信号 | 在线数、点赞和弹幕量级不同，原值相加会被最大项支配 | 统一降低边际收益并自然处理 0 | 权重仍需真实数据训练/实验，不是业务真理 |
| 点赞历史计数与排名贡献分限额 | 用户应能连点表达情绪，但不能无限购买榜单曝光 | 每秒入口限流保护系统，每分钟热度上限保护排序 | 只是一层基础反刷，不等于成熟风控 |
| Redis ZSet 榜单 | 热榜需要高频更新和 Top-N 读取，不适合每几秒写 MySQL 排序 | ZSet 原生维护 score 排序，读取复杂度和接口简单 | Redis 故障需降级；大规模全量重建需增量化/分片 |
| 榜单按开播时间降级 | 排名系统故障不能让首页不可用或重新被历史点赞支配 | `status,start_at` 可由 MySQL 索引稳定查询，也给新房基本曝光 | 降级排序质量较低，应监控降级持续时间 |
| Redis Lua 原子缓存后发布 | 分步命令可能让客户端先收到通知却补不到数据 | 同一脚本保证缓存、裁剪、TTL、热度计数和发布的局部原子性 | 不能跨 Kafka/MySQL 原子；慢 Lua 会阻塞 Redis |
| 最近 ZSet + MySQL 两级补拉 | 只靠 Pub/Sub 无法恢复，所有补拉都查 MySQL又太慢 | Redis 承担短窗口高频恢复，过期后 MySQL兜底 | 固定 TTL 有雪崩风险，热房轮询会形成热 Key |
| 三类 Client Channel | 弹幕、控制和状态的丢弃语义不同 | 事件 FIFO、控制优先、状态覆盖，避免旧数据阻塞恢复指令 | 调度是近似优先；控制队列满仍需断开 |
| 有界队列 + 丢旧保新 | 无限排队会把慢客户端问题转成全局 OOM 和高延迟 | 限定单连接内存，最新消息优先，旧缺口通过 replay 补 | 单客户端实时流不再逐条完整，依赖补拉正确性 |
| room+shard 固定 worker | 全房串行无法利用多核，随机并行又会打乱单连接顺序 | 房间内按连接分片并行，每个连接固定进入一个 FIFO worker | 热点可能分布不均，worker 数必须压测而非拍脑袋 |
| PreparedMessage 复用 | 同一 JSON 给 N 个观众重复编码浪费 CPU 和内存 | 一次编码、复用帧对象，符合广播内容相同的特点 | 个性化消息不能共享，需要另走路径 |
| 热房采样推 + 拉补偿 | 纯推会产生 QPS×在线数放大，纯拉会形成高延迟和请求风暴 | 正常全推，超预算采样，缺口再拉，按压力动态切换 | 可能把压力转移到 Redis/HTTP，必须错峰和限流 |

这张表是面试叙述的主线：每说一个技术名词，都应同时说出它保护的业务语义、替代方案为什么没选，以及当前实现在哪个条件下需要升级。

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
| 累计点赞长时间不变 | realtime like batch 指标、checkpoint、rooms.like_count | 聚合事务失败或消费 Lag 过高 |
| 房间排名不更新 | 热榜任务日志、在线心跳、分钟桶、rooms:hot | Redis 故障、计算任务退出或 MySQL 开播房间查询失败 |

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
- 房间列表读取热榜失败时会按 MySQL 开播时间降级；`like_count` 仍以 MySQL 为真相源，不会随热榜丢失。

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
- 实时 Group 的弹幕分支不依赖 MySQL，已有 WebSocket 仍可继续看已消费到的弹幕；点赞聚合分支依赖 MySQL，并且与弹幕共用 Topic/Group，持续数据库故障最终会阻塞该实时消费循环。
- 热榜任务无法重新查询开播房间时会保留上一次 `rooms:hot`，不会主动写空榜；但房间列表补详情本身仍依赖 MySQL。

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

- 入口 Lua 先预留 message_id，Kafka 确认后转为带 TTL 的 accepted 标记，吸收客户端重试。
- Kafka 消费幂等不依赖这个临时标记，而由 MySQL 分区 checkpoint 保证；聚合事务同时更新 like_batches、rooms.like_count 和 last_offset。
- 代价是 MySQL 不再永久保存每次点击的 message_id；如需长期逐点击审计，应把 Kafka 原始事件下沉到分析存储。

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
3. 点赞的客户端 message_id 去重只有 Redis TTL；Redis 丢失后再次生产同一逻辑点击仍可能重复计数。
4. room_seq 绑定 partition offset，Kafka 扩分区会破坏单调性。
5. 一人一房只靠先查后插，缺少 owner_id 唯一索引。
6. 优雅退出没有等待 Hub、Subscriber 和两个 Consumer 完成；HTTP Shutdown 也不会自动关闭已 Hijack 的 WebSocket。
7. 配置和 Compose 中含明文凭据，不应进入生产仓库。

### P1：性能与可观测性

1. persistence 是逐条 Count+Insert，不是真正批量 upsert。
2. Redis replay 热房轮询可能形成热点 Key。
3. Redis Pub/Sub 的无订阅者窗口可能静默丢实时通知。
4. 实时热度权重仍是工程初值，没有真实业务实验、反作弊模型和推荐效果评估。
5. 多实例都会运行同一个热榜重算任务，没有 leader election；当前属于近似幂等覆盖，生产应独立调度。
6. 旧历史查询 room_id + id 与现有 room_id + created_at 索引不匹配。
7. 每个 WebSocket 握手都会查房间；带 Session 的连接还会查 Redis 和 MySQL 用户，可增加缓存或握手令牌。
8. 指标当前是 JSON 内存计数和日志，不是 Prometheus 指标，也没有告警。

### P2：代码质量和协议

1. rousters、UserReigister、ErrInvalidEamil 等命名拼写需修正。
2. GetRoomDetail 忽略 room_id 解析错误，部分错误状态码使用 500。
3. StartRoomRequest 没有 required/min/max 校验。
4. onlineUsers 维护了账号计数，但广播使用 len(clients)，名称“在线人数”实际是连接数。
5. ACK 目前只记录，不驱动服务端重传或端到端成功判定。

## 14. 排序策略复盘与业务验证

当前房间列表只取 `status=1`，但已经不再按 MySQL `like_count` 排序：

- `rooms.like_count` 是 Kafka 聚合后持久化的历史展示值；
- Redis 分钟桶近似统计最近一分钟点赞与弹幕，各网关用带 TTL 的心跳汇总在线连接；
- 后台任务每 3 秒按 `log1p` 压缩后的在线量、近期点赞、近期弹幕和开播时间衰减计算 `hot_score`，写入 `rooms:hot` ZSet；
- API 按 Redis 顺序批量补齐 MySQL 房间详情；Redis 不可用时按开播时间降级，不让历史点赞重新支配排序；
- 同一用户每分钟最多为排名贡献 10 次点赞，但所有有效点击仍会累计到历史 `like_count`。

当前公式为：

    score = 3 × log(1 + online_connections)
          + 2 × log(1 + recent_likes)
          + 1 × log(1 + recent_danmas)
          + 1 × exp(-live_minutes / 30)

权重是可配置的工程初值，尚未经过真实流量实验。下一阶段可继续演进为：

- 用独立流计算作业替代应用内定时聚合；
- 在线连接进一步区分游客设备与登录用户，降低多开连接影响；
- 增加异常行为、账号质量和风险惩罚项；
- 通过曝光、停留、互动等线上指标校准权重。

### 14.1 怎么证明解耦真的解决了业务问题

当前源码只能证明排序机制已改变，不能证明用户更喜欢这个榜单。真实验证应至少设计三组实验：

| 实验组 | 排序方式 | 用途 |
|---|---|---|
| Control A | `like_count DESC` | 验证历史累计锁榜问题是否真实存在 |
| Control B | `start_at DESC` | 判断仅给新房曝光能否改善发现效率 |
| Treatment | 当前 `hot_score` | 验证多信号实时排序是否优于两个简单基线 |

主要效果指标：榜单曝光到进房点击率、进房后 30 秒停留率、弹幕/点赞互动转化率。护栏指标：新房曝光占比、头部曝光集中度、榜单位置抖动、用户投诉/刷量率，以及 Redis/MySQL P95。只有 Treatment 在效果指标上稳定提升且护栏不恶化，才能把“架构合理”升级为“业务有效”。

### 14.2 当前能量化、不能量化的价值

- 能量化：100 个同房间点赞在理想聚合窗口内可把 MySQL 房间行更新从最多 100 次降到 1 次；热榜读取不再执行 `ORDER BY like_count DESC`；Redis 故障有确定的开播时间降级路径。
- 需实测：真实批次填充率、MySQL 事务下降比例、热榜刷新耗时、降级率和不同房间规模下的 Redis 开销。
- 不能声称：点击率、停留、留存或推荐效果提升，因为当前没有真实用户实验。

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
15. 实现点赞聚合、持久 checkpoint、Redis 对账恢复。
16. 拆分累计点赞与实时热度，加入分钟桶、跨网关在线心跳、ZSet 榜单和读取降级。
17. 增加 pprof、链路指标、严格压测和故障注入。

### 15.3 生产化版本

18. 改造稳定房间序列，消除扩分区风险。
19. 后台任务 supervisor、leader election、readiness、告警和优雅退出等待。
20. 通用限流、消息校验、审核、禁言和风控。
21. Kafka/Redis/MySQL 高可用与凭据管理。
22. 将点赞原始事件下沉分析存储，并把单节点 Redis 限流升级为可观测的风控服务。
23. 用真实 A/B 数据校准热榜权重，加入风险惩罚和多样性策略。

## 16. 测试核验结果

2026-09-03 在当前工作区执行：

- go test ./...：通过。
- go vet ./...：通过。
- go test -race ./...：未执行成功，当前 Windows Go 环境未启用 CGO，race detector 要求 CGO。
- 覆盖率：config 约 90.7%，middleware 约 47.2%，kafka 约 7.8%，ws 约 51.1%，Repo 约 18.2%，DAO 约 3.2%，hotrank 约 8.1%；auth/handler/service 仍基本为 0%。

这说明核心 Hub 并发行为、点赞 checkpoint 规划、热度公式和榜单重组已有回归测试，但登录、权限、真实数据库事务、Redis Lua 与端到端链路仍缺足够自动化测试。测试通过不能替代真实 MySQL/Redis/Kafka 集成测试，也不能证明旧数据库 schema 正确。

## 17. 复盘口径

### 17.1 可以主动讲的亮点

- 用 Kafka 双消费组把低延迟广播和 MySQL 耐久解耦。
- 用 room_id 分区和固定 shard worker 把 Kafka 分区顺序延续到单客户端。
- 用三类队列表达事件、控制和可覆盖状态的不同语义。
- 用 ZSet + Lua + replay + message_id 实现可补偿的至少一次链路。
- 从 O(N²) 在线状态广播、channel close race 和 Kafka 默认 batching 延迟等真实问题出发迭代。
- 用严格的 expected/received、P95 和 Lag，而不是模糊的“高并发”做性能判断。
- 用聚合 delta + 持久 checkpoint 降低连续点赞的数据库热点，并把重放幂等从易变的内存批次边界中解耦。
- 把 `like_count` 的历史展示语义与 `hot_score` 的实时曝光语义拆开，并为热榜设计 Redis 故障降级。

### 17.2 必须主动承认的边界

- 历史 10,000 连接数据不是当前最新架构的完整复测结果。
- Kafka/Redis/MySQL 都是本地开发级单点配置。
- Pub/Sub 不是可靠队列；完整性来自 Kafka、最近缓存、历史库和客户端恢复的组合，但仍有静默缺口。
- room_seq 方案不支持无痛扩 Kafka 分区。
- 点赞累计值与热榜已闭环，但逐点击永久审计和 Redis 去重窗口之外的逻辑幂等没有实现。
- 项目没有真实业务指标，只能讲工程指标。

### 17.3 如果重来一次

1. 先定义 SLO：accepted P95、端到端 P95、缺口率、恢复时间、房间规模。
2. 先设计稳定 room_seq 和消息协议，再选 Kafka 分区。
3. 一开始就做输入校验、限流和毒消息隔离。
4. 把后台任务纳入统一生命周期和健康检查。
5. 先搭可重复集成环境与压测判定，再做性能优化。
6. 点赞从第一天就按事件幂等和聚合一致性设计。
7. 排行榜上线前先定义业务目标与 A/B 基线，而不是先拍权重；同时把独立用户、风险惩罚和榜单抖动纳入指标。

## 18. 待本人补证

以下内容不能从源码得出，面试前必须填实：

- 项目是独立完成还是协作完成，各自边界是什么。
- 最初为什么选择直播弹幕，目标岗位或学习目标是什么。
- 哪个问题最难、本人如何定位、做过哪些错误判断。
- 历史压测所用服务器 CPU、内存、网卡、系统参数和压测机配置。
- 最新架构部署后的新基线数据。
- 热榜三组排序实验的点击率、30 秒停留率、互动转化和曝光集中度。
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
- 点赞聚合：internal/repo/like.go、internal/kafka/realtime_consumer.go
- 实时热榜：internal/hotrank/engine.go、internal/dao/hot_ranking.go、internal/repo/room.go
- 参考客户端：web/danmaku-client.js
- 压测：cmd/benchmark、scripts/load-test.ps1
- 可靠协议：docs/reliable-danmaku-protocol.md
- 历史压测复盘：docs/autumn-recruitment-project-review.md
- 变更证据：docs/automated-load-test-change-log.md
