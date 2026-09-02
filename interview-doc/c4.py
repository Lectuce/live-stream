# -*- coding: utf-8 -*-
"""第 4 章 Redis Pub/Sub 跨节点分发"""

BLOCKS = [
    ("h1", "四、Redis Pub/Sub 跨节点分发"),
    ("p", "水平扩展后，同一房间的连接分散在不同服务节点上，单节点内存路由无法把消息送到其他节点上的连接。"
          "Redis Pub/Sub 充当节点之间的“实时广播总线”：每个节点既是发布者也是订阅者。"),

    ("h2", "4.1 发布、订阅与按需订阅（引用计数）"),
    ("code", """// 发布端：弹幕先写 Kafka 成功，再 PUBLISH（对应“已持久化弹幕再分发”）
rdb.Publish(ctx, "room:"+roomID, payload)

// 订阅端：每个被订阅的房间一个订阅循环，Channel() 内部自动重连
func (n *Node) subscribe(roomID string) {
    sub := n.rdb.Subscribe(ctx, "room:"+roomID)
    ch := sub.Channel()
    for msg := range ch { // 注意：重连窗口期内的消息会丢失
        n.hub.dispatch(&BroadcastJob{
            RoomID:  roomID,
            Payload: []byte(msg.Payload),
        }) // 只做“搬到本地有界队列”一件事，不做重活
    }
}

// 按需订阅：用本地引用计数管理海量小房间
type subRef struct {
    mu   sync.Mutex
    refs map[string]int // roomID -> 本节点连接数
}
func (s *subRef) enter(id string) {
    s.mu.Lock(); defer s.mu.Unlock()
    s.refs[id]++
    if s.refs[id] == 1 { go node.subscribe(id) } // 本地第一个人进房才订阅
}
func (s *subRef) leave(id string) {
    s.mu.Lock(); defer s.mu.Unlock()
    s.refs[id]--
    if s.refs[id] == 0 { // 最后一个人离开才退订，避免无效订阅
        delete(s.refs, id); node.unsubscribe(id)
    }
}"""),

    ("h2", "4.2 面试问答"),

    ("q", "Q25【做】★★★ 两台节点各持有一半连接，A 节点用户发的弹幕怎么到 B 节点用户？完整走一遍。"),
    ("a", "A 节点 ReadPump 收到弹幕并持久化后，向 Redis 频道 room:R PUBLISH；A、B 两个节点都订阅了该频道，"
          "于是都会收到这条消息。A 节点收到后广播给本地连接（包括发送者自己，用于回显和统一顺序），"
          "B 节点的订阅 goroutine 收到后投递进本地 jobCh，由 Worker Pool 经房间快照送到 B 的 send 队列，"
          "最终 WritePump 写给用户。Redis 在这里只做跨节点转发，不做持久化。"),

    ("q", "Q26【因】★★ 为什么强调“先持久化再 PUBLISH”，顺序反过来有什么问题？"),
    ("a", "若先 PUBLISH 再写 Kafka：用户已经在屏幕上看到弹幕，但随后写 Kafka 失败（重试耗尽、进程崩溃），"
          "这条弹幕就永久丢失且无法对账——用户看到过的内容数据库里却没有。先写 Kafka 成功再分发，"
          "保证“凡是被实时展示的弹幕，一定已经可靠落地”，实时分发失败也只是个别用户漏看，可通过历史消息补回。"
          "代价是实时链路多等一次 Kafka ACK，可用 Async 批量和就近分区把延迟压到很低。"),

    ("q", "Q27【做】★★ 订阅 goroutine 的代码结构？收到消息后怎么找到本地房间？"),
    ("a", "见 4.1：Subscribe 后从 Channel() 持续读消息，频道名里带 roomID（room:{id}），"
          "解析出 roomID 后通过 Hub 的房间路由表查到本地 Room，再 dispatch 进有界队列。"
          "订阅 goroutine 必须非常“薄”：只搬运、不广播、不写库，避免阻塞订阅通道，"
          "重活全部交给下游 Worker Pool。"),

    ("q", "Q28【做】★★ 是启动时订阅全部房间吗？海量小房间怎么按需订阅？"),
    ("a", "不可能启动即订阅全部房间（房间数可能百万级，绝大多数节点上无人）。用本地引用计数："
          "本节点某房间第一个连接进入时才 SUBSCRIBE，最后一个连接离开时 UNSUBSCRIBE（见 4.1）。"
          "这样每个节点只订阅“本机确实有人”的房间，订阅数量与本机连接的房间数同阶。"
          "超大热门房间则所有节点都订阅，属正常情况。"),

    ("q", "Q29【障】★★★ 订阅连接闪断 3 秒，这期间的消息去哪了？"),
    ("a", "Redis Pub/Sub 是 fire-and-forget、至多一次（at-most-once）：没有消费者在线的消息不会被保存，"
          "go-redis 的 Channel() 虽然会自动重连，但断线期间 PUBLISH 的消息直接丢弃、不会补投。"
          "影响是这 3 秒内用户漏看部分弹幕。要主动承认这是设计内的可靠性取舍，并给出缓解："
          "客户端可按最后收到的序列号拉取近期历史弹幕补洞；监控订阅重连事件并告警。"),

    ("q", "Q30【因】★★★ 既然会丢，为什么跨节点广播不用 Redis Stream 或 Kafka，而用 Pub/Sub？"),
    ("akey", "两条链路诉求不同——",
          "实时链路要的是毫秒级延迟和极简扇出，弹幕是“流”，漏看个别几条用户无感、也不需要重放，"
          "Pub/Sub 无持久化开销、延迟最低；落库链路要的是绝不丢、可重放、可回溯，所以用 Kafka。"
          "Stream/Kafka 每条消息落盘、消费要维护 offset/消费组，用来做全量实时广播会引入不必要的延迟和复杂度。"
          "这是按可靠性目标分层，而不是一个中间件包打天下。"),

    ("q", "Q31【障】★★ Redis 整体不可用，实时分发怎么办？"),
    ("a", "首先 Redis 自身做高可用（哨兵 Sentinel 或 Cluster，客户端自动故障转移）。应用层兜底："
          "①本节点内的消息仍可直接走本地房间广播，保证同节点用户可见，只是跨节点暂时不通；"
          "②订阅/发布带超时与熔断，失败不阻塞弹幕主流程，更不能影响 Kafka 落库；"
          "③恢复后由客户端通过历史消息接口补齐缺口。要明确：Redis 故障只影响跨节点实时性，不影响数据最终落库。"),

    ("q", "Q32【做】★ 频道怎么命名、消息体怎么序列化？"),
    ("a", "频道按房间隔离，命名 room:{roomID}，需要做全局广播可另设 system 频道或用 PSUBSCRIBE 模式订阅。"
          "消息体建议用 Protobuf（或 JSON）封装：msgID、roomID、userID、类型、内容、服务端时间戳、序列号，"
          "定长字段前置、压缩体积；高吞吐场景 Protobuf 比 JSON 更省带宽和序列化 CPU。"),
]
