# -*- coding: utf-8 -*-
"""第 3 章 房间路由与并发模型"""

BLOCKS = [
    ("h1", "三、房间路由与并发模型：房间锁、客户端快照、Worker Pool、有界队列"),
    ("p", "本章对应简历前半句，是并发功底的集中考察区。核心矛盾是：如何在高频广播下，"
          "既不让共享的客户端集合被并发读写破坏，又不让任何一个慢客户端拖住整个房间，更不能让内存被慢消费者拖爆。"),

    ("h2", "3.1 房间结构、客户端快照与慢连接摘除"),
    ("code", """type Room struct {
    id      string
    mu      sync.RWMutex          // 房间级读写锁
    clients map[*Client]struct{}  // 房间内全部连接
    jobCh   chan *BroadcastJob    // 有界广播任务队列
}

// 注册/注销走写锁，临界区极短
func (r *Room) remove(c *Client) {
    c.once.Do(func() {
        r.mu.Lock()
        delete(r.clients, c) // 先从路由表摘除
        close(c.send)        // 再关闭发送队列
        r.mu.Unlock()
    })
}

// 广播：锁内只做快照拷贝，锁外做网络发送
func (r *Room) broadcast(payload []byte) {
    r.mu.RLock()
    snapshot := make([]*Client, 0, len(r.clients)) // 客户端快照
    for c := range r.clients {
        snapshot = append(snapshot, c)
    }
    r.mu.RUnlock() // 持锁时间 = 一次内存拷贝，不含任何阻塞发送

    var slow []*Client
    for _, c := range snapshot {
        select {
        case c.send <- payload: // 队列未满，立即投递
        default:                // 队列已满 => 慢连接
            slow = append(slow, c) // 先收集，禁止在 RLock 内注销
        }
    }
    for _, c := range slow {
        r.remove(c) // 锁外统一摘除，触发 WritePump 退出
    }
}"""),

    ("h2", "3.2 广播 Worker Pool 与两级有界队列"),
    ("code", """type BroadcastJob struct {
    RoomID  string
    Payload []byte
}

// 节点启动时开固定数量 worker，从共享有界任务队列取活
func (h *Hub) startWorkers(n int) {
    for i := 0; i < n; i++ {
        go func() {
            for job := range h.jobCh { // jobCh = make(chan *BroadcastJob, 1024)
                if room := h.getRoom(job.RoomID); room != nil {
                    room.broadcast(job.Payload)
                }
            }
        }()
    }
}

// 上游（Redis 订阅 goroutine）投递任务也要非阻塞
func (h *Hub) dispatch(job *BroadcastJob) {
    select {
    case h.jobCh <- job:
    default:
        h.dropCount.Add(1) // 节点级过载：丢弃 + 打点告警，绝不阻塞订阅协程
    }
}"""),
    ("p", "两级有界队列：第一级是节点/房间的广播任务队列 jobCh，第二级是每个 Client 的发送队列 send。"
          "两级都是有界 channel，构成自上而下的背压（backpressure）传导链。"),

    ("h2", "3.3 面试问答"),

    ("q", "Q14【做】★★ Room 的数据结构定义出来；注册、注销、广播分别怎么拿锁？"),
    ("a", "见 3.1：clients 用 map 集合 + sync.RWMutex。注册/注销改变集合，走 Lock 写锁，临界区只做 map 增删和 "
          "channel 关闭；广播只遍历集合，走 RLock，且临界区只做一次快照拷贝。读多写少，多个广播可同时持 RLock 并发拷贝。"),

    ("q", "Q15【做】★★★ “客户端快照”具体是哪几行？为什么持锁时间必须压到一次 map 拷贝？"),
    ("a", "即 RLock 下把 clients 复制成一个 slice 后立刻 RUnlock。若持锁逐个向 client.send 发送，"
          "而某个客户端网络卡顿导致 send 队列已满，发送会阻塞（或被迫等待），于是读锁一直不释放，"
          "此时该用户进房/退房要拿写锁就会被堵，最终整个房间的注册、广播全部卡死。"
          "快照把锁内操作变成纯内存拷贝（微秒级），把不可控的网络/channel 等待全部移到锁外，"
          "从根上消除“一个慢客户端拖垮一个房间”。"),

    ("q", "Q16【障】★★★ RLock 里发现某客户端 send 满了，能直接在锁里调 remove 注销吗？"),
    ("a", "不能，会死锁。当前 goroutine 持有的是 RLock（读锁），remove 需要 Lock（写锁），"
          "Go 的 sync.RWMutex 不可升级，同一 goroutine 在等自己释放读锁后才能拿写锁，形成自锁。"
          "正确做法是 default 分支先把慢连接收集到 slice，释放 RLock 之后在锁外统一 remove。"),

    ("q", "Q17【因】★★ 为什么是房间级 RWMutex，而不是全局一把 Mutex？"),
    ("a", "锁粒度决定并发度：全局大锁会让不同房间的广播互相阻塞，A 房间卡顿波及 B 房间；"
          "房间级锁把竞争限制在同一房间内，不同房间完全并行，符合“房间之间天然隔离”的业务结构。"
          "选 RWMutex 是因为广播（读集合）频率远高于进出房（写集合），多读单写场景读锁可并发，吞吐更高。"),

    ("q", "Q18【做】★★ Worker Pool 怎么启动、worker 数怎么定？全局一个池还是每房间一个？"),
    ("a", "节点启动时创建固定 N 个 worker goroutine，共同消费有界 jobCh（见 3.2）。"
          "worker 数不按房间数开，而是按 CPU 核数和广播任务是“内存拷贝+channel 投递”这类 CPU/调度型任务来定，"
          "初始取 GOMAXPROCS 附近，再用压测看 CPU、队列延迟和调度延迟调优。"
          "一般用节点级共享池即可，避免海量小房间各自开 worker 造成 goroutine 膨胀；超大房间可再做房间级隔离，"
          "防止单房间占满共享池。"),

    ("q", "Q19【做】★★★ 非阻塞投递的 select-default 写出来；两级队列满了分别怎么处理，策略为什么不同？"),
    ("a", "写法见 3.1/3.2 的 select{case ch<-x: default:}。策略分层：①客户端 send 满 → 判定为慢连接并主动注销，"
          "因为保留一个长期跟不上的连接只会让它延迟越积越大，且故障必须被隔离在单个连接；"
          "②节点广播 jobCh 满 → 说明整体过载，选择丢弃当前任务并打点/告警，绝不能阻塞 Redis 订阅 goroutine，"
          "否则一个节点的消费停滞会让所有房间的跨节点消息都收不到，故障被放大到整节点。"),

    ("q", "Q20【因】★★★ 客户端拥塞时为什么选“注销慢连接”，而不是阻塞等待或丢弃这一条消息？"),
    ("akey", "三种策略对比——",
          "阻塞等待会让广播 worker 被该连接占住，拖慢全房间；只丢这一条而保留连接，"
           "会让队列长期处于将满状态、消息持续零星丢失且延迟不断累积，问题被掩盖；"
           "主动注销是“故障隔离”：慢消费者的发送速率已跟不上房间速率，继续保留没有价值，"
           "断开后客户端可重连并通过历史消息接口追弹幕，整体广播延迟立刻恢复。"
           "阈值（send 容量）是“容忍短时网络抖动”和“及时摘除慢节点”之间的折中。"),

    ("q", "Q21【因】★★ 为什么不每条弹幕直接 go broadcast() 起一个 goroutine？"),
    ("a", "弹幕高峰每秒数万条，每消息一个 goroutine 会导致 goroutine 数量瞬时爆炸，带来巨大调度开销和 GC 压力；"
          "更致命的是没有背压上限——下游再慢也会无限创建 goroutine、无限堆积待发数据，最终 OOM。"
          "固定 Worker Pool + 有界队列把并发度和内存占用都约束在确定上界内，过载时队列满会显式触发丢弃/摘除策略。"),

    ("q", "Q22【做】★★ send=64、jobCh=1024 这类 buffer 大小怎么来的？"),
    ("a", "按“峰值速率 × 可容忍抖动时长”估算，而不是拍脑袋。例如单连接峰值约 30 条/秒、希望容忍约 2 秒瞬时抖动，"
          "send 取 64 左右；jobCh 按节点广播峰值速率 × worker 短暂抖动窗口估到百/千量级。"
          "面试时给出推导口径比给出固定数字更重要，并说明最终值由压测（观察队列水位和 P99 延迟）校正。"),

    ("q", "Q23【障】★★ 做快照的一瞬间正好有用户进房/退房，会漏发或重发吗？接受这个一致性吗？"),
    ("a", "快照是某一时刻的集合拷贝：拷贝后新进房的用户收不到这一条（下一条开始能收到），"
          "刚退房但已在快照里的用户可能被投递一次（其 send 已被 close 的情况不会发生，因为摘除先于 close，"
          "且遍历到已关闭 channel 的路径被“先摘 map”切断）。这是一个极短的最终一致窗口。"
          "弹幕是高频、弱一致、过期无价值的流，完全可以接受，不值得为单条弹幕的线性一致付出全局锁代价。"),

    ("q", "Q24【放】★ 每次广播都 copy 一次 map，超高 QPS 下 GC 压力怎么优化？"),
    ("a", "优化方向：①用“不可变快照 + 原子指针替换”，集合变更时复制后整体替换，广播方拿到只读快照无需加锁、零重复拷贝；"
          "②对象池 sync.Pool 复用 slice；③分片锁降低竞争；④客户端集合在两次广播间不变时缓存快照。"
          "能主动提到“每消息分配会增加 GC 负担”并给出方向即可，属于加分项。"),
]
