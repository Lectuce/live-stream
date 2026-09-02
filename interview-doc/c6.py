# -*- coding: utf-8 -*-
"""第 6 章 Kafka Async Writer + Consumer Group 幂等落库"""

BLOCKS = [
    ("h1", "六、Kafka 异步缓冲与 Consumer Group 幂等落库"),
    ("p", "弹幕写入是突发洪峰，让 MySQL 直接承接每秒数万次单行 INSERT 会把磁盘 IO 和连接池打满。"
          "Kafka 在这里承担削峰填谷与解耦：生产端 Async Writer 攒批快速缓冲，消费端按自己的节奏批量、幂等落库。"),

    ("h2", "6.1 生产端：kafka-go Async Writer"),
    ("code", """w := &kafka.Writer{
    Addr:         kafka.TCP(brokers...),
    Async:        true,                       // 异步：WriteMessages 只入内存缓冲
    Balancer:     &kafka.Hash{},              // 按 Key 的哈希选分区
    BatchSize:    100,                        // 攒够 100 条成一批
    BatchTimeout: 20 * time.Millisecond,      // 或攒到 20ms 也成批发出
    RequiredAcks: kafka.RequireAll,           // ISR 全部确认才算成功
    Completion: func(msgs []kafka.Message, err error) {
        if err != nil { metrics.KafkaFail.Inc() } // Async 唯一的错误感知入口
    },
}
// Key = roomID：同一房间固定落同一分区 => 分区内有序
w.WriteMessages(ctx, kafka.Message{
    Key:   []byte(roomID),
    Value: payload,
})
// 优雅退出：收到 SIGTERM → 停止读新消息 → w.Close() 阻塞等待内存批次 flush"""),

    ("h2", "6.2 消费端：Consumer Group 攒批 + 幂等写 MySQL"),
    ("code", """r := kafka.NewReader(kafka.ReaderConfig{
    Brokers: brokers, Topic: "danmu", GroupID: "danmu-persister",
})
for {
    batch := make([]kafka.Message, 0, 500)
    deadline := time.Now().Add(500 * time.Millisecond)
    // 攒批：凑够 500 条 或 到达 500ms 超时，先到先触发
    for len(batch) < 500 {
        ctx, cancel := context.WithDeadline(context.Background(), deadline)
        m, err := r.FetchMessage(ctx)
        cancel()
        if err != nil { break } // 超时即落库，保证低峰期也不积压太久
        batch = append(batch, m)
    }
    if len(batch) == 0 { continue }

    // 一条多值 INSERT，靠 msg_id 唯一索引幂等
    // INSERT IGNORE INTO danmu(msg_id, room_id, user_id, content, created_at)
    //   VALUES (?,?,?,?,?),(?,?,?,?,?),...
    if err := repo.BatchInsertIgnore(toRecords(batch)); err != nil {
        continue // 写库失败：不提交 offset，下轮重新拉取重放（幂等保证不重复）
    }
    r.CommitMessages(context.Background(), batch...) // 先写库成功，再提交 offset
}"""),
    ("code", """-- 幂等的根基：消息唯一键唯一索引
ALTER TABLE danmu ADD UNIQUE KEY uk_msg_id (msg_id);
-- 重复 msg_id 的行被静默跳过；若重复时需要刷新字段则改用 ON DUPLICATE KEY UPDATE"""),

    ("h2", "6.3 面试问答"),

    ("q", "Q38【因】★★ 为什么不直接写 MySQL，中间要加 Kafka？"),
    ("a", "三个作用：①削峰填谷——弹幕是突发写洪峰，Kafka 承接瞬时尖峰，消费端按 MySQL 能承受的速率匀速批量写入；"
          "②解耦——在线服务不等磁盘 IO，写 Kafka（Async）几乎立刻返回，落库慢/故障不影响用户发弹幕和实时观看；"
          "③批量与重放——攒批把上万次单行 INSERT 合并成少量多值 INSERT，显著降低事务和 IO 次数，且消息可重放，"
          "落库失败、下游修复后能重新消费。"),

    ("q", "Q39【做】★★ Async Writer 的 BatchSize/BatchTimeout/Balancer 怎么配？怎么按房间保序？"),
    ("a", "BatchSize 控制每批条数、BatchTimeout 控制最长等待，二者先到先触发，用来平衡吞吐和延迟；"
          "Balancer 选 kafka.Hash 并把消息 Key 设为 roomID，则同一房间的弹幕永远哈希到同一分区，"
          "Kafka 只保证单分区内有序，于是同房间弹幕在该分区内严格按写入顺序排列。"
          "RequiredAcks 设为 RequireAll（ISR 全部确认）以兼顾不丢。"),

    ("q", "Q40【障】★★★ Async 模式写入失败你怎么知道？进程被 kill -9，内存 buffer 里的消息怎么办？"),
    ("akey", "Async 下 WriteMessages 不返回真实发送结果，",
          "错误只能在 Completion 回调里拿到（失败计数、重试/告警都在这里做），这是很多人答不上的细节。"
          "Async 的固有窗口是：已进入 Writer 内存批次、但尚未真正发到 Broker 的消息，在进程 panic/kill -9/掉电时会丢。"
          "缓解手段：①优雅退出——捕获 SIGTERM 后停止收新消息并调用 Close() 阻塞等内存批次 flush 完再退出；"
          "②重要场景可对关键消息改 Sync 写或本地 WAL 兜底；③客户端对未收到服务端 ack 的弹幕做有限重发，配合 msgID 幂等不重复。"
          "要主动承认 Async 用极小的崩溃窗口换了极高吞吐。"),

    ("q", "Q41【做】★★ Consumer 怎么“攒一批”？批量 SQL 长什么样、一批多少？"),
    ("a", "用“条数上限 + 时间上限”双阈值攒批（见 6.2）：凑够如 500 条，或距首批 500ms 还没凑够也落库，"
          "避免低峰期消息在内存久等。SQL 用一条多值 INSERT：INSERT INTO ... VALUES (),(),... 一次发送多行，"
          "批次大小（500/1000）按 max_allowed_packet、单事务耗时和写入吞吐压测确定，远优于循环单行 INSERT。"),

    ("q", "Q42【做】★★★ 消息唯一键在哪生成、怎么生成？为什么不能在消费端生成？"),
    ("a", "必须在入口（ReadPump 收到弹幕、进入异步链路之前）就生成并随消息一路携带，一般用雪花算法"
          "（时间戳 + 节点 ID + 自增序列）得到全局趋势递增、全局唯一的 msgID。"
          "如果在消费端生成，那么“写库成功但 offset 未提交、重放再次消费”时会生成新 ID，幂等键每次都不同，"
          "重复消息无法被识别，幂等设计直接失效。重传、重试、跨节点都必须复用同一个 msgID。"),

    ("q", "Q43【做】★★ INSERT IGNORE 和 ON DUPLICATE KEY UPDATE 有什么区别？唯一索引建在哪？"),
    ("a", "二者遇到唯一键冲突时都不报错：INSERT IGNORE 直接跳过冲突行（适合弹幕这种重复了就忽略的场景）；"
          "ON DUPLICATE KEY UPDATE 会用新值更新已有行（适合需要幂等 upsert、修正字段的场景）。"
          "唯一索引建在 msgID 列（uk_msg_id）；若业务还要求“同一用户同一内容短时间不重复”，可另建联合唯一键，"
          "但幂等去重的主键是全局 msgID。"),

    ("q", "Q44【因】★★★ offset 先提交还是先写库？“写库成功、提交前宕机”会怎样？"),
    ("akey", "必须先写库成功、再手动提交 offset（见 6.2）。",
          "Kafka 默认至少一次（at-least-once）投递：若写库成功但 CommitMessages 之前进程宕机，"
          "重启后会从上次已提交 offset 重新拉取，这批消息被重复消费、重复写库；由于 uk_msg_id 唯一索引 + "
          "INSERT IGNORE，重复行被跳过，结果不变。这就是“至少一次投递 + 消费端幂等 = 效果上恰好一次（effectively-once）”。"
          "反过来若先提交 offset 再写库，宕机会造成消息永久丢失，是错误顺序。"),

    ("q", "Q45【障】★★ 一批里写库失败怎么办？会不会堵死整个分区？重试耗尽呢？"),
    ("a", "写库失败时不提交 offset，下轮重新拉取重放（幂等保证安全）；为避免一条坏消息反复卡死整批，"
          "可把大批次拆小重试以定位问题行，对问题行做格式/长度校验跳过。多次重试仍失败（如 DB 持续不可用）时，"
          "进入退避重试并告警，必要时转死信队列/死信表，不能无限空转阻塞分区消费，同时保留消息不丢。"),

    ("q", "Q46【障】★★ 消费 lag 持续上涨，排查和扩容的顺序是什么？"),
    ("a", "先定位瓶颈：①消费并行度上限 = 主题分区数，消费者组内多于分区数的实例闲置，扩容消费者前先确认/增加分区；"
          "②看批量写库是否慢（慢 SQL、单条 INSERT、索引过多、锁等待），增大批次、优化 SQL；"
          "③看消费者是否被外部调用阻塞、GC、负载；④看批次超时是否过于保守。"
          "顺序是先让单消费者更快（批量化、异步化），再用“分区数 = 并行度上限”为界水平加消费者。"),

    ("q", "Q47【做】★★ 同一房间弹幕顺序怎么保证？跨分区或客户端乱序怎么兜底？"),
    ("a", "服务端以 roomID 为 Key 哈希到固定分区，保证同房间消息在单分区内生产顺序 = 消费顺序 = 落库顺序；"
          "跨房间分散到不同分区本就不需要全局有序。端侧兜底：消息体带服务端单调序列号/时间戳，"
          "客户端收到后按序列号排序、去重、补洞，抵消网络多路径和重连导致的局部乱序。"),

    ("q", "Q48【放】★ 历史弹幕回看从哪查？表和索引怎么设计？"),
    ("a", "历史弹幕查 MySQL（或进一步同步到分析型存储/缓存）。弹幕表按 room_id + created_at 建联合索引支撑"
          "“某房间某时间段翻页”，高写入场景可按房间或时间分表；热门房间的近期弹幕可放 Redis 缓存加速首屏加载。"
          "这也正好补上 Pub/Sub 断线漏消息的缺口：客户端按最后序列号拉历史补洞。"),
]
