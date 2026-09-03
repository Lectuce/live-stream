# live-stream 面试问答：项目、场景与基础知识

> 使用方式：先练 Q1～Q20，保证项目能完整讲清；再练故障场景；最后用 Go、网络、操作系统、Redis、Kafka、MySQL 八股承接追问。
>
> 回答中的“当前实现”均指 2026-09-02 仓库 HEAD。带“生产演进”的内容是设计建议，不能说成已经实现。

## 一、项目介绍与架构

### Q1：一句话介绍这个项目。

回答：这是一个 Go 直播互动后端，提供用户 Session、直播房间和实时弹幕能力；弹幕经 WebSocket 接入，以 Kafka 作为可靠交接点，通过实时和持久化两个 Consumer Group 分别完成 Redis 跨实例广播与 MySQL 历史落库，并用最近消息缓存、客户端去重和补拉处理断线与慢客户端缺口。

### Q2：它解决的核心业务问题是什么？

回答：第一，主播和观众需要身份、房间和开关播闭环；第二，弹幕要低延迟展示又要可追溯；第三，热门房间的一对多扇出和慢客户端不能拖垮系统；第四，断线或主动降级后要能补回消息。这个项目没有真实商业指标，价值应表述为工程闭环和可验证容量，不能虚构 DAU 或收入。

### Q3：为什么不能把它说成完整直播平台？

回答：它实现的是直播互动和房间控制，不包含 RTMP/WebRTC 推流、转码、录制、CDN、真实 HLS 服务和内容审核。PlayURL 只是示例域名拼接，因此准确名称是“直播互动后端”或“实时弹幕系统”。

### Q4：项目的四层架构是什么？

回答：HTTP 业务面是 Handler、Service、Repo、DAO。Handler 处理 Gin 参数和响应；Service 做房主权限、房间状态机和分页规则；Repo 表达用户、房间、弹幕的查询与持久化；DAO 初始化和封装 MySQL、Redis、Lua 与迁移。实时弹幕是与四层正交的事件数据面，不应强行说成完全经过四层。

### Q5：实时数据面的模块边界是什么？

回答：ws/handler 负责握手，ws/client 负责单连接读写和背压，kafka/producer 负责可靠交接，两个 Consumer 分别负责实时与持久化，Redis Subscriber 把跨实例消息送入本机，Hub 负责房间路由和扇出，参考 JS 客户端负责重连、去重和补拉。

### Q6：为什么选 Go？

回答：系统有大量长连接和独立生命周期任务，Go 的 goroutine、Channel、net/http 和成熟的 WebSocket/Kafka/Redis 客户端适合构建 I/O 密集型服务。更关键的是 Go 让每连接读写协程模型直观，但这不意味着 goroutine 免费，仍要控制阻塞、队列、内存和退出。

### Q7：为什么需要 Kafka、Redis 和 MySQL 三种存储/中间件？

回答：Kafka 负责耐久交接、顺序和削峰；Redis Session 支持高频鉴权，Pub/Sub 提供低延迟跨实例通知，ZSet 保存最近恢复窗口；MySQL 保存用户、房间和可长期查询的弹幕历史。三者不是重复，而是分别解决交接、实时和长期状态。

### Q8：项目最值得讲的三个工程问题是什么？

回答：一是集中建连时在线人数逐次广播形成 O(N²)，后来改为 dirty room 定时合并；二是广播与注销并发导致 send on closed channel，后来用 sendMu 和 sync.Once 修复；三是低流量端到端延迟接近秒级，最终定位到 kafka-go 默认 BatchTimeout，并显式设为 5ms。

### Q9：当前性能结论能怎么讲？

回答：历史版本证明过单机 10,000 WebSocket 全量建连，以及 1,000 连接下约 9,800 次投递/秒零缺口；10,000 连接更高投递率出现过递增缺口。最新推拉、自适应和 PreparedMessage 改造后没有同环境全链路复测，所以只能把旧数据说成历史证据，不能说成当前最终上限。

### Q10：如果面试官问你的个人贡献？

回答：源码只能证明项目内容，不能证明个人边界。回答必须按实际情况拆成“我独立设计和实现”“我主导但与谁协作”“我只参与哪一段”，再用一个故障定位案例和一组可验证指标证明贡献，不能笼统说“整体负责”。

## 二、接口、数据与核心链路

### Q11：一条弹幕的完整端到端流程是什么？

回答：登录用户通过 WebSocket 发消息；服务端校验或生成 message_id，并覆盖 room_id、user_id 和 username；Producer 以 room_id 为 Key 同步写 Kafka；实时 Group 为消息补 room_seq，Lua 原子写 Redis 最近 ZSet 并 Pub/Sub；本机 Subscriber 送入 Hub，Hub 按房间和 16 个连接分片扇出；另一个 Group 事务写 MySQL，成功后提交自己的 offset。

### Q12：accepted 代表什么？

回答：它只代表 Kafka WriteMessages 成功并满足当前 RequiredAcks，不代表 MySQL 已落库，也不代表任何观众已经收到。端到端成功要分别观测 Kafka、Redis、Hub 入队和客户端接收。

### Q13：为什么实时展示不直接等 MySQL？

回答：MySQL 写入延迟和故障会直接拉高直播互动延迟。两个 Consumer Group 把实时 Redis 路径与持久化 MySQL 路径解耦，数据库抖动只增加 persistence Lag，已有连接仍可看实时消息；代价是观众已看到但历史短时间查不到的最终一致性。

### Q14：两个 Consumer Group 和同一 Group 两个 Consumer 有什么区别？

回答：两个 Group 会各自消费完整 Topic，适合“实时广播”和“历史持久化”两个不同下游。一个 Group 内多个 Consumer 会分摊分区，同一条消息只交给组内一个成员，用于横向扩容同一种处理。

### Q15：room_seq 是什么，为什么不是连续的？

回答：当前 room_seq 是 Kafka partition offset+1。同一房间固定进同一分区，所以它对该房间单调；但同分区还有其他房间，它们会占用 offset，因此对单房间允许跳号。客户端只能比较大小，不能把 last_seq+1 不存在直接当作丢消息。

### Q16：顺序是怎样从 Kafka 保持到 WebSocket 客户端的？

回答：room_id Key + Hash Balancer 保证同房间进入同分区；Kafka 保证分区内顺序；Hub 将每个 room+shard 固定映射到 FIFO worker；同一连接固定属于一个 shard，并只有一个 writePump。任一环节如果随机分配 worker，都可能让同一客户端乱序。

### Q17：为什么同一广播只序列化一次？

回答：房间广播的 JSON 内容对所有观众相同。Hub 创建一个 gorilla/websocket PreparedMessage，再让所有目标连接复用，避免 N 个连接执行 N 次 JSON 编码和帧准备；队列也只保存指针与 room_seq，降低复制和 GC 压力。

### Q18：三类客户端队列分别是什么？

回答：send 容量 256，保存弹幕事件；control 容量 8，保存 accepted、error、resync 和策略切换；state 容量 1，在线人数和点赞只保留最新值。控制队列优先读取，避免恢复指令被旧弹幕堵住。

### Q19：历史分页和补拉为什么是两套接口？

回答：普通历史浏览按 MySQL id 倒序向过去翻页，参数是 before_id；实时恢复按 room_seq 升序向未来追赶，参数是 after_seq。两者方向、排序和语义不同，混成一个 cursor 容易出错。

### Q20：客户端如何避免“拉历史期间实时消息到达”造成错乱？

回答：补拉期间先把实时消息放进 liveBuffer；按页获取 replay，按 message_id 去重并推进 room_seq；补拉结束后把 liveBuffer 按 room_seq 排序再投递。这样把“历史快照”和“实时尾部”合并成一条有序流。

## 三、可靠性、幂等、推拉与故障排查

### Q21：系统是 exactly-once 吗？

回答：不是。Kafka 到 Redis/MySQL 都按至少一次设计，发布或事务成功后提交 offset，崩溃窗口会重放。系统用 message_id 唯一键和客户端去重承受重复；这叫“至少一次交付 + 幂等效果”，不是端到端 exactly-once。

### Q22：弹幕在哪些地方可能重复？

回答：Producer 返回不确定错误后客户端重试；Redis 发布成功但 offset 未提交时进程崩溃；MySQL 事务成功但持久化 offset 未提交时崩溃。消息必须全链路保留同一 message_id，不能每次重试生成新 ID。

### Q23：MySQL 幂等是怎么做的？

回答：danmas.message_id 有唯一索引，消费者事务内先 Count，存在则 no-op，不存在再 Insert。更好的实现是直接批量 INSERT ... ON DUPLICATE KEY UPDATE/IGNORE，减少往返并让数据库唯一约束成为最终裁判。

### Q24：点赞如何同时实现高吞吐与 Kafka 重放幂等？

回答：入口用 Redis Lua 预留 message_id，避免客户端普通重试重复写 Kafka；消费者按 100 条或 200ms 聚合，不保存逐点击 MySQL 明细。事务内每个房间写一条 like_batches(delta)、原子增加 rooms.like_count，并推进 Kafka 分区 checkpoint。即使数据库提交后、offset 提交前崩溃，重启后的批次边界改变，checkpoint 仍会跳过已经处理的 offset。代价是 message_id 去重只覆盖配置的 Redis TTL；Kafka 重放幂等则是持久的。

### Q25：Redis Lua 脚本保证了什么？

回答：它把 ZADD、窗口裁剪、EXPIRE 和 PUBLISH 放在一次 Redis 原子执行中，保证消息在发布前已进入可恢复窗口。它不保证 Redis 操作与 Kafka offset 提交原子，因此仍可能重放。

### Q26：Redis Pub/Sub 丢消息怎么办？

回答：Pub/Sub 本身不存离线消息。当前用 Kafka 保留源事件、Redis ZSet 保存最近窗口、MySQL 保存长期历史，并由客户端在重连或收到 resync 后补拉。但本实例 Subscriber 短暂离线时可能出现静默缺口，生产上还需要订阅自愈和可连续的房间 watermark/序列来主动发现。

### Q27：慢客户端为什么不能简单把队列调大？

回答：大队列只把故障延后，还会按连接数放大内存和 GC，最终延迟更高。当前策略是有界队列、丢旧保新、发送 resync、持续积压才断开，把完整性转移给补拉链路。

### Q28：为什么热房要推拉结合？

回答：纯推的成本是源 QPS×在线人数，热门房间会把很低的输入放大成巨大扇出；纯拉会增加延迟和请求风暴。普通房间全推，单连接缺口局部拉，热房超预算时采样推加错峰拉，是按负载动态折中。

### Q29：推拉结合有什么新风险？

回答：可能把 WebSocket 压力转移成 Redis 热 Key 和 HTTP 轮询风暴。需要抖动、限流、请求合并、节点内共享房间拉取器、批量响应和热点分片，不能只看到 WebSocket 投递下降。

### Q30：服务端如何判断热房？

回答：每个房间按时间窗统计源消息 QPS，乘以本机连接数得到预计 fanout QPS；超过预算则计算 ceil(fanout/budget) 作为采样步长，最大 16。在线人数达到 5,000 或采样步长大于 1，也会标记为 hot。

### Q31：如果 Redis 故障，链路怎么表现？

回答：实时 Consumer 在缓存/发布处重试且不推进 offset，Kafka realtime Lag 上升；持久化 Group 仍可落 MySQL；新 Session 鉴权会失败。如果 Redis Subscriber 已退出，当前 main 只记日志不拉起，Redis 恢复后该实例仍需重启，这是明确缺陷。

### Q32：如果 Kafka 故障，链路怎么表现？

回答：启动时 Leader 探测失败会终止启动。运行中 WriteMessages 可能变慢或返回错误，客户端收到 503 后应复用 message_id 重试；当前生产调用没有单消息超时，可能让 readPump 长时间阻塞，生产上应加 deadline、熔断和限流。

### Q33：如果 MySQL 故障，为什么观众仍可能看到弹幕？

回答：实时和持久化使用不同 Consumer Group。持久化 Group 会在事务处重试并积累 Lag，但实时 Group 仍能把 Kafka 消息送到 Redis 和 WebSocket。若 Redis 最近窗口过期而 MySQL 仍未追平，补拉完整性会受影响。

### Q34：Kafka Lag=0，为什么客户端仍可能有缺口？

回答：Lag=0 只表示 Consumer 已处理到最新 offset，不表示 Hub 队列已排空、Socket 已写完、网络已送达或压测机已读取。还要看 ws_client_queue_depth、queue_full、write_errors、端到端 received/expected 和客户端资源。

### Q35：所有用户 accepted 很慢，怎么排查？

回答：先看 kafka_producer_write_duration；再看 Broker 可达性、acks、ISR、网络和 BatchTimeout。若延迟接近固定周期，优先怀疑 batching；若随负载上升，检查 Broker 磁盘、请求队列和客户端 goroutine 堆积。

### Q36：accepted 正常，但所有观众收到很慢，怎么排查？

回答：比较 Kafka message age、Redis publish duration 和 ws realtime message age。Kafka age 高说明实时 Consumer 拉取慢；Redis publish 高说明 Redis；到 Subscriber 已经快但客户端慢，则看 Hub 分派、fanout shard、队列深度和网卡。

### Q37：只有少数观众丢消息，怎么排查？

回答：优先看这些连接的队列压力、resync 数、write timeout 和客户端网络，而不是先查 Kafka。若同一房间所有实例上的某一批都缺，再查 Redis Subscriber 与 Pub/Sub；若历史也没有，再查 persistence Lag 和 MySQL。

### Q38：如何排查重复消息？

回答：以 message_id 为主键，在 Producer 日志、Kafka、Redis payload、MySQL 和客户端去重指标中串联。还要区分“服务端重复投递”和“压测器把每个观众各收到一次错误地当成重复”。

### Q39：毒消息会怎样？

回答：非法 JSON 当前会记录后提交，等于丢弃；能反序列化但违反数据库约束的消息可能让持久化批次无限重试，阻塞该消费者处理。生产上要入口校验、Schema 版本、重试上限、批次拆分和 DLQ。

### Q40：如何做优雅退出？

回答：应先停止接收新请求和新 WebSocket，通知 Producer/Consumer/Subscriber 停止，刷新待处理批次和 offset，关闭所有已 Hijack 的 WebSocket，等待 WaitGroup，再关闭外部连接。当前代码发出 cancel 后没有等待后台任务，Hub.Stop 也不遍历关闭连接，仍需完善。

## 四、Go 高频追问

### Q41：每个 WebSocket 为什么通常有 readPump 和 writePump 两个 goroutine？

回答：gorilla/websocket 允许一个并发 reader 和一个并发 writer。readPump 处理客户端输入和 Pong，writePump 串行写数据、状态、控制和 Ping；所有业务 goroutine 只向队列投递，避免多个 goroutine 并发写同一连接。

### Q42：Channel 关闭后读写语义是什么？

回答：从已关闭 Channel 读取会立即得到零值和 ok=false；向已关闭 Channel 发送会 panic；重复 close 也会 panic。项目用 sendMu+sendClosed 让发送与关闭互斥，并用 Once 保证注销和连接关闭只发生一次。

### Q43：为什么不能用 recover 解决 send on closed channel？

回答：recover 只是吞掉 panic，没有修复生命周期竞态，可能造成丢消息、泄漏和状态不一致。正确做法是建立 Channel 所有权和同步规则，让 close 与 send 不并发。

### Q44：Mutex、RWMutex 和 atomic 怎么选？

回答：需要维护多个字段不变量时用 Mutex；读多写少且临界区明确时用 RWMutex；单个计数、标志和游标可用 atomic。不要用多个 atomic 拼复杂一致性，也不要默认 RWMutex 一定更快。

### Q45：项目为什么既有 Hub 全局锁又有 Room 锁？

回答：Hub 锁只保护 room_id 到 Room 的字典增删；Room 锁保护单房间客户端、分片和状态。这样不同房间可以并行，避免一个热门房间持锁阻塞全部房间。

### Q46：广播时为什么复制客户端快照后释放锁？

回答：网络队列投递和客户端关闭可能较慢，持有 Room 锁会阻塞注册、注销和其他广播。复制指针快照后释放锁，再逐个 trySend；Client 自己的 sendMu 负责关闭并发安全。

### Q47：context 在项目里承担什么职责？

回答：根 Context 用于通知 Consumer、Subscriber 和 Hub 退出，HTTP Request Context 用于数据库和 Session 操作。生产 Kafka 时当前误用了 Hub 长生命周期 Context，应该为单次写创建带超时的子 Context。

### Q48：select 有多个 ready case 时顺序固定吗？

回答：不固定，会做伪随机选择，因此不能仅靠 case 顺序保证优先级。writePump 先用一个非阻塞 select 主动探测 control，再进入主 select，才实现控制消息的近似优先。

### Q49：sync.Once 适合解决什么？

回答：适合“一生最多执行一次”的动作，例如发送注销信号、关闭 Socket。它不替代完整状态机；如果 Once 内动作失败且需要重试，就不适合直接使用。

### Q50：goroutine 泄漏常见原因是什么？

回答：永久阻塞的 Channel 发送/接收、无超时网络调用、Ticker 未 Stop、Context 不传播、后台任务退出无人等待。这个项目要特别检查 Kafka WriteMessages、Consumer fetch goroutine 和 WebSocket 关闭路径。

### Q51：Go map 能并发读写吗？

回答：普通 map 不能并发读写，可能直接 fatal。Hub.rooms、Room.clients 和 policy 都由锁保护；只读快照要在锁内复制，锁外使用。

### Q52：有界 worker pool 的价值是什么？

回答：限制并发任务数和队列内存，避免每条消息或每个客户端无限创建 goroutine。项目为每个 Hub 建 CPU×2 个 worker 和容量 4096 队列；代价是过载时必须定义等待、丢弃或降级策略。

### Q53：Go GC 对长连接系统有什么影响？

回答：大量连接、Channel、短生命周期 JSON 和日志对象会提高堆大小与扫描成本，引发延迟抖动。PreparedMessage 复用、固定桶直方图、减少逐客户端日志和控制队列大小都有助于降低分配，但必须用 pprof 和 gctrace 验证。

### Q54：怎么定位 CPU 和内存问题？

回答：CPU 用 pprof profile 看热点和调用图；内存看 heap、allocs 和对象保留；阻塞看 block profile，锁竞争看 mutex profile，goroutine profile 查泄漏。压测时还要同步采集网卡和客户端资源，否则容易把接收端瓶颈误判为服务端。

### Q55：errors.Is 和 errors.As 的区别？

回答：Is 用于判断错误链中是否匹配某个哨兵错误或 Is 规则；As 用于提取某个具体错误类型。项目用 As 提取 MySQLError 1062，用 Is 判断 GORM duplicated key 和业务哨兵错误。

## 五、计算机网络

### Q56：WebSocket 握手发生了什么？

回答：客户端先发 HTTP/1.1 Upgrade 请求，带 Upgrade、Connection、Sec-WebSocket-Key 等头；服务器返回 101 Switching Protocols，随后同一 TCP 连接传输 WebSocket 帧。项目在 Upgrade 前校验 Origin、room_id、房间存在和开播状态。

### Q57：CORS 和 WebSocket Origin 校验一样吗？

回答：不一样。CORS 是浏览器对普通跨域 HTTP 响应的访问控制；WebSocket 握手不受标准 CORS 同样约束，服务端必须显式校验 Origin，防止跨站 WebSocket 劫持。项目两者共用 allowed_origins 白名单。

### Q58：WebSocket 基于 TCP，为什么还要 Ping/Pong？

回答：TCP 连接可能因 NAT、代理、网络半开而长时间不显式报错。应用层 Ping/Pong 能确认对端活性并刷新读 deadline；项目 pongWait=60 秒，pingPeriod 为其 90%。

### Q59：TCP 为什么会发生粘包，WebSocket 为什么不用自己按消息长度拆？

回答：TCP 是字节流，没有应用消息边界；WebSocket 协议在其上定义帧头、长度和消息边界，库负责组帧和拆帧。应用仍要设置 ReadLimit，防止超大消息占用内存，当前项目尚未设置。

### Q60：慢客户端在网络层意味着什么？

回答：对端读取慢会让内核发送缓冲区填满，最终 Write 阻塞或超时。如果业务广播持锁同步写，一个慢连接会拖住所有人；项目用每连接队列和独立 writePump 隔离，并设置 write deadline。

### Q61：为什么 10,000 连接不等于 10,000 QPS？

回答：连接数是并发状态，QPS 是单位时间事件数。直播还要区分输入 QPS 和广播投递率；10 条/秒发给 10,000 人是约 100,000 次投递/秒。

### Q62：TIME_WAIT 是什么，高并发建连时为什么关注？

回答：主动关闭 TCP 的一方会在 TIME_WAIT 保留一段时间，防止旧报文污染新连接并保证最后 ACK 可重传。短连接压测会消耗大量临时端口；WebSocket 是长连接，但反复失败重连仍可能造成端口压力。

### Q63：单台压测机约 60,000 连接为什么容易失败？

回答：同一目标四元组受本地临时端口范围限制，还受内存、句柄和网卡影响。应多源 IP 或多压测机分摊，并区分客户端资源耗尽与服务器容量。

### Q64：TLS 会带来什么成本？

回答：握手增加 CPU 和往返，数据加解密增加 CPU，证书与连接状态增加内存。长连接能摊薄握手成本；生产应使用 wss，并把 TLS 终止位置和代理超时纳入压测。

### Q65：反向代理部署 WebSocket 要注意什么？

回答：必须转发 Upgrade/Connection，配置足够长的 idle/read/write timeout，关闭不合适的响应缓冲，并传递真实 IP。多实例不一定要求粘性会话，因为 Redis Pub/Sub 会广播到各实例，但房间在线数和热房策略当前只统计本机。

## 六、操作系统

### Q66：高并发网络服务为什么使用 I/O 多路复用？

回答：一个线程可等待大量 fd 的就绪事件，而不是每个连接阻塞一个 OS 线程。Go runtime 的 netpoller 在 Linux 使用 epoll、macOS 使用 kqueue、Windows 使用 IOCP，并把就绪事件调度给 goroutine。

### Q67：goroutine 和线程的区别？

回答：goroutine 初始栈小、由 Go runtime M:N 调度，切换通常比线程轻；线程由内核调度、栈和上下文更重。阻塞系统调用、CGO 和高 CPU 任务仍会占用线程，因此 goroutine 不是无限资源。

### Q68：文件描述符为什么会限制 WebSocket？

回答：Linux 中每个 Socket 是 fd，进程和系统都有上限。连接数上万时还会使用监听 fd、Kafka/Redis/MySQL 连接和日志文件，必须检查 ulimit -n 和系统级限制；Windows 对应句柄和端口资源。

### Q69：用户态和内核态缓冲分别在哪里产生背压？

回答：项目的 send Channel 是用户态队列；gorilla 写入后进入内核 Socket 发送缓冲；网络再进入网卡队列。只看 Channel 深度不能证明网络无积压，必须结合 write latency、Socket 指标和网卡吞吐。

### Q70：上下文切换过多有什么表现？

回答：CPU 利用率高但有效吞吐不升，调度器和 syscall 占比高，P95/P99 抖动。应减少 goroutine 风暴、批量处理、避免过细锁和高频定时器，用 profile 验证。

### Q71：进程收到 SIGTERM 后应该做什么？

回答：先标记不就绪和停止接流，再完成或超时终止在途任务，刷新批次，关闭长连接，最后释放资源。必须有总超时，避免永远等慢客户端；当前代码有 10 秒 HTTP Shutdown，但未完整等待后台协程。

### Q72：OOM 时为什么不能只怪单条大消息？

回答：长连接固定成本、每连接 256 队列、Kafka 预取、JSON 分配、Socket 缓冲和积压会共同放大。要用 heap profile 按对象类型和保留路径分析，并按连接数与队列高水位做容量模型。

### Q73：Docker 网段冲突为什么会导致服务端端口无法访问？

回答：主机路由按最长前缀匹配。如果 Docker bridge 网段和客户端局域网重叠，回包可能被路由到 bridge 而不是物理网卡。项目历史上把 Compose 网段改到 10.250.0.0/24 规避此问题。

## 七、Redis

### Q74：为什么 Session 放 Redis，不直接用 JWT？

回答：Session 可主动注销、封禁和立即失效，服务端权限变更容易生效；Redis 查询快且支持 TTL。JWT 无状态但撤销和刷新更复杂。项目把随机 Token 放 HttpOnly Cookie，只在 Redis 保存 Token 哈希。

### Q75：Redis Session 的安全边界是什么？

回答：Token 用 crypto/rand 生成，Redis Key 是 SHA-256(Token)，数据库不保存 Token。生产仍要 HTTPS、Secure/HttpOnly/SameSite、CSRF 防护、登录限流、凭据密钥管理和 Session 轮换。

### Q76：为什么最近消息用 ZSet？

回答：score=room_seq，可按 after_seq 做开区间范围查询并保持顺序，还能按 rank 裁剪最老记录。List 适合按位置，但按游标范围和重复覆盖不如 ZSet 直接。

### Q77：Redis Pub/Sub 和 Streams 的区别？

回答：Pub/Sub 低延迟、广播给在线订阅者，但不持久化、不确认、不重放；Streams 保存消息、支持 Consumer Group、ACK 和 pending。当前已经有 Kafka 做耐久源，因此用 Pub/Sub 做通知，但订阅空窗仍要补偿。

### Q78：什么是缓存穿透、击穿、雪崩？

回答：穿透是反复查不存在数据并回源；击穿是热点 Key 失效时大量请求同时回源；雪崩是大量 Key 同时失效或 Redis 故障造成整体回源。本项目要对非法 room_id 做前置校验/空缓存，对热房 replay 做请求合并，对固定 TTL 加抖动。

### Q79：如何解决热房 replay 的 Redis 热 Key？

回答：客户端请求随机抖动只是第一步。服务端可按 room_id+cursor singleflight、节点内缓存同页结果、房间级统一拉取后本地分发、热点分片和限流；若数据量更大，可将补拉服务独立扩容。

### Q80：Redis 单线程为什么还能快？

回答：核心命令执行路径单线程，避免锁和上下文切换，主要操作在内存并用高效事件循环；网络 I/O 和持久化在新版本也有后台机制。大 Key、慢 Lua 和 O(N) 命令仍会阻塞所有请求。

### Q81：Lua 脚本有什么风险？

回答：执行期间阻塞 Redis 主线程，脚本过长或遍历大集合会拖慢所有客户端；还要注意参数校验和集群 Key 必须同 slot。当前脚本操作单个房间 ZSet，复杂度主要来自 ZCARD/ZREMRANGEBYRANK，窗口固定较小。

### Q82：Redis 持久化 RDB 和 AOF 怎么选？

回答：RDB 快照恢复快、文件紧凑，但可能丢最后一个快照窗口；AOF 记录写命令，数据更完整但文件和恢复成本更高。当前 Compose 开启 AOF，但点赞仍不能只依赖单实例 Redis 作为业务真相。

### Q83：Redis Cluster 对这个项目有什么影响？

回答：Session 和不同房间 Key 可自然分片，但单个超大房间仍是单 Key 热点。Lua 涉及的 Key 必须在同一 hash slot；可用 room_id hash tag 保证同房间相关 Key 共槽，但这也会集中热点。

### Q84：缓存更新为什么常讨论一致性？

回答：数据库和缓存双写没有天然事务，顺序不同会产生脏读或丢更新。当前点赞以 MySQL rooms.like_count 为真相源，聚合事务成功后写 Redis 的绝对值，再广播状态，最后提交 Kafka offset；中途失败可以靠 Kafka 重放和 MySQL checkpoint 重试。它仍是最终一致，不是跨 MySQL、Redis、Kafka 的分布式事务。

## 八、Kafka

### Q85：Kafka 为什么适合弹幕交接？

回答：它提供持久日志、分区内有序、Consumer Group、可重放和削峰，能把 WebSocket 接入与实时/持久化下游解耦。它不负责最终 WebSocket 投递，也不自动提供全局顺序。

### Q86：为什么必须同时设置 Key 和 Hash Balancer？

回答：Key 只是消息元数据，Balancer 决定如何选分区。如果使用 LeastBytes，同一 Key 仍可能进入不同分区；Hash 才会按 Key 稳定路由，让同 room_id 获得分区内顺序。

### Q87：acks=all/RequireAll 保证什么？

回答：Leader 等待当前 ISR 中副本确认后再成功，耐久性高于只等 Leader。当前 Docker 只有一个 Broker、复制因子 1，所以 RequireAll 没有真正的多副本容灾价值；生产必须配合副本数和 min.insync.replicas。

### Q88：同步 Producer 和异步 Producer 怎么选？

回答：同步能把 Broker 确认结果直接反馈给发送者，错误语义清晰，但吞吐和延迟受每次写影响；异步吞吐高，但 accepted、失败回调和进程退出刷盘更复杂。当前用同步 + 5ms batching，优先明确可靠交接。

### Q89：BatchSize 和 BatchTimeout 如何影响延迟与吞吐？

回答：批次大能提高压缩和网络效率，但低流量时要等超时。项目曾因默认 1 秒 timeout 出现固定秒级延迟，改为 5ms。参数要按业务 P95 SLO 和 Broker 吞吐共同压测，不能只追求大 batch。

### Q90：Consumer Group 如何分配分区？

回答：一个分区在同一 Group 内同一时刻只分配给一个 Consumer；Consumer 数超过分区数会有空闲。两个不同 Group 会各读一份完整消息。扩 Consumer 前要先评估分区数和单分区热点。

### Q91：什么时候提交 offset？

回答：业务副作用成功后提交。实时组在 Redis 缓存发布成功后推进，持久化组在 MySQL 事务成功后推进；先提交再处理会在崩溃时丢数据，处理后提交会在崩溃窗口产生重复。

### Q92：为什么提交批次时要传所有分区的消息？

回答：Kafka offset 按 Topic-Partition 独立。只传批次最后一条会漏掉其他分区的进度；kafka-go 会为每个分区提交传入消息中的最高 offset。

### Q93：什么是 Rebalance，有什么风险？

回答：消费者加入、退出或分区变化时，Group Coordinator 重新分配分区。Rebalance 期间消费暂停；若处理和提交边界不清，可能重复处理。长事务和无限重试也会影响组稳定性，应有合理超时和幂等。

### Q94：Kafka 如何保证顺序？

回答：只保证单分区追加顺序，不保证 Topic 全局顺序。生产并发重试、改变 Key、扩分区或下游并行处理都可能破坏业务顺序，所以项目还需要固定 room+shard worker 和单连接 writer。

### Q95：Kafka 扩分区为什么会破坏当前 room_seq？

回答：Hash 对分区数取模，分区数变化后同一 room_id 可能映射到新分区，而新分区 offset 与旧分区无关，可能从更小值开始。当前数据库 room_id+room_seq 唯一和客户端单调游标都会出问题，生产必须先改序列方案再扩分区。

### Q96：Kafka Lag 高怎么定位？

回答：先分 Group 和 Partition，看是实时还是持久化、单热点还是全部；再比较消费处理耗时、Redis/MySQL 延迟、批次、重试、Rebalance 和 Broker fetch。Lag 是结果指标，必须与吞吐和下游延迟关联。

### Q97：如何处理毒消息？

回答：反序列化失败可记录原因后送 DLQ，再提交原消息；业务约束失败应有限重试并隔离，不能无限卡住分区。DLQ 要保存原 Topic、Partition、Offset、Key、Payload、错误和 Schema 版本。

### Q98：Kafka 能保证消息绝不丢吗？

回答：不能脱离配置和故障模型回答。需要生产端 acks、重试与幂等、Broker 副本和 ISR、磁盘持久化、正确提交 offset、下游幂等和监控共同保证；单 Broker replication=1 仍有单点丢失风险。

## 九、MySQL

### Q99：为什么历史消息使用游标分页，不用 OFFSET？

回答：深 OFFSET 需要扫描和丢弃大量前置行，越往后越慢；并发插入还会造成重复或漏读。WHERE id < before_id ORDER BY id DESC LIMIT N 能利用有序索引，复杂度和稳定性更好。

### Q100：复合索引最左前缀是什么？

回答：索引 (a,b,c) 通常可支持以 a 开头的条件组合，如 a、a+b、a+b+c；跳过 a 很难有效使用。当前 replay 的唯一索引 (room_id,room_seq) 正好支持 room_id=? AND room_seq>? ORDER BY room_seq。

### Q101：当前历史索引有什么问题？

回答：模型有 (room_id,created_at)，但旧历史接口按 room_id 和 id 倒序；更匹配的索引是 (room_id,id)。应通过 EXPLAIN、数据规模和回表成本验证，而不是只凭索引名称判断。

### Q102：房间热榜需要什么索引？

回答：查询是 WHERE status=1 ORDER BY like_count DESC LIMIT N，可考虑 (status,like_count DESC) 组合索引，并根据选择性和覆盖列评估。当前更大的问题是 like_count 没从 Redis 回写，索引再好也排序不准。

### Q103：为什么“一人一房”必须有数据库唯一约束？

回答：Service 的“先查 owner_id 再插入”在并发请求下存在竞态，两个请求都可能查不到后成功插入。给 rooms.owner_id 加唯一索引，应用把 1062 转成业务冲突，才能让数据库守住不变量。

### Q104：事务能解决所有并发问题吗？

回答：不能。事务只保证其隔离范围内的 ACID，仍要选择隔离级别、锁和唯一约束。Count 后 Insert 即使在事务里也可能并发冲突，唯一索引才是最终保证。

### Q105：MySQL 常见隔离级别是什么？

回答：Read Uncommitted、Read Committed、Repeatable Read、Serializable。InnoDB 默认 Repeatable Read，通过 MVCC 和必要的锁处理一致性；隔离越强并发成本通常越高，需按业务不变量选择。

### Q106：MVCC 是什么？

回答：多版本并发控制让读事务基于一致性视图读取历史版本，减少读写互斥；InnoDB 通过事务 ID、undo log 和 ReadView 判断版本可见性。当前用户/房间普通查询主要受益于快照读，但状态原子更新仍需条件写或锁。

### Q107：什么是覆盖索引和回表？

回答：查询所需列都在二级索引中时可以直接返回，叫覆盖索引；否则先由二级索引找到主键，再查聚簇索引，叫回表。热榜返回列较多，完全覆盖索引可能过宽，应权衡写放大。

### Q108：为什么批量拉取不等于批量写？

回答：当前消费者收集 100 条后在一个事务里循环 Count+Insert，减少了事务提交，但 SQL 仍是逐条执行。真正批量写应构造多值 INSERT/upsert，并处理重复键和单条毒数据的隔离。

### Q109：数据库迁移为什么不能只靠 AutoMigrate？

回答：AutoMigrate 擅长新增表列和部分索引，不会安全推断历史数据如何回填，也不一定修复已有错误主键。项目对 room_seq 先按房间回填，再建唯一索引，就是数据感知迁移；生产还需要版本化、可回滚和备份。

### Q110：MySQL 连接池要关注什么？

回答：最大打开连接、最大空闲连接、连接最大生命周期和等待时间。连接太少会排队，太多会压垮 MySQL；还要给查询 Context 超时，监控活跃/空闲/等待、慢 SQL、锁等待和事务时间。

## 十、系统设计升维

### Q111：如何估算单房间容量？

回答：至少估算连接固定内存、客户端队列、源 QPS、fanout=源 QPS×连接数、平均帧大小、出站带宽、序列化 CPU 和慢客户端比例。例：10 条/秒×10,000 人=100,000 次投递/秒；1KiB 负载就接近 98MiB/s，尚未含协议与 TLS。

### Q112：如何横向扩 WebSocket 节点？

回答：负载均衡把连接分到多个节点，Redis Pub/Sub 把每条实时消息送到所有节点，每个节点只向本机房间连接扇出。生产还需全局 presence 聚合、订阅自愈、节点健康摘除和避免所有节点处理无本地连接房间的消息。

### Q113：单个超级热点房间怎么继续扩？

回答：跨节点分散连接只能分摊 Socket 扇出，但每节点仍收到所有房间消息。可增加房间级路由与订阅、边缘 fanout、分层广播树、服务端房间拉取器、消息采样/摘要，以及按地域拆分；不能无限增加单节点 worker。

### Q114：如何设计稳定的房间序列？

回答：选择取决于吞吐和扩容：Redis INCR 简单但需高可用且热点；数据库 sequence 强一致但吞吐受限；Snowflake 只近似有序；Kafka partition+offset 是稳定复合游标但客户端协议更复杂；也可由房间 actor 单写生成。当前项目最重要的是先把单 uint64 与 Kafka 分区数解耦。

### Q115：房间排行榜怎么设计？

回答：不能只按累计点赞。可用近期点赞、在线人数、弹幕率和时间衰减组合评分，Redis ZSet 保存实时榜单，流处理或定时任务计算窗口，MySQL保存快照和基础详情；接受秒级最终一致，但要做反刷和风险降权。

### Q116：这个系统的 SLO 应怎么定？

回答：分阶段定义：Kafka accepted P95、端到端可见 P95/P99、严格测试缺口率、重连恢复时间、replay 成功率、两个 Group Lag 水位、单房间/单机连接与投递预算。没有 SLO，“高并发”和“低延迟”都无法验收。

### Q117：如何做容量扩展决策？

回答：先通过阶梯压测找 CPU、网卡、Hub 队列、Redis、Kafka 或压测机中的首个瓶颈，再选择纵向、横向或协议降级。不要在证据不足时同时改 worker 数、队列、batch 和网络参数，否则无法归因。

### Q118：如何设计可观测性？

回答：指标看速率、错误、延迟和饱和度；日志以 message_id、room_id、partition、offset 关联；Trace 跨 WebSocket 接入、Kafka、Redis 和回放；告警覆盖 Producer 错误、两个 Lag、订阅存活、队列高水位、resync、P95/P99 和 MySQL 慢事务。高基数 room_id 不应无控制地做指标标签。

### Q119：如果要求“绝不丢一条弹幕”，你怎么回答？

回答：先澄清是 accepted 前、accepted 后、在线实时展示还是最终历史。分布式系统无法承诺所有故障下绝对不丢；可通过耐久交接、多副本、幂等、重试、补拉、审计和 SLO 把风险降到目标，但客户端断电或业务主动限流仍要定义语义。

### Q120：这个项目最成熟的通用方法论是什么？

回答：先区分事件、状态和控制消息；再为每类定义顺序、持久性、背压和补偿语义；用消息 ID 与游标建立可观测证据链；最后用连接数、输入率、扇出率、延迟和缺口分别压测。技术选型应服务于明确语义，而不是为了堆中间件。

## 十一、面试前最后自测

能脱稿回答以下问题，才算完成源码内化：

1. 不看代码画出 HTTP 四层和弹幕实时/持久化两条链路。
2. 说出全部路由、三张表、四类 Redis 对象、一个 Topic 和两个 Group。
3. 从 readPump 一直说到 writePump，并解释每次 ACK/offset 的准确含义。
4. 写出 Client 三队列、Hub 两级锁、room+shard worker 的代码骨架。
5. 解释 room_seq 为什么单调不连续、为什么扩分区会坏。
6. 用一个 message_id 排查 accepted 慢、全体慢、局部缺口和历史缺失。
7. 说清缓存击穿、Kafka 故障、幂等、推拉结合和点赞一致性。
8. 严格区分当前源码、历史实测和未来设计，不把建议说成已实现。
