# -*- coding: utf-8 -*-
"""第 2 章 连接层 ReadPump / WritePump"""

BLOCKS = [
    ("h1", "二、连接层：ReadPump / WritePump 双协程模型"),
    ("p", "gorilla/websocket 的官方约定：一个连接同一时刻只能有一个 goroutine 读、一个 goroutine 写。"
          "因此每个连接固定开两个 goroutine：ReadPump 专职读上行并在退出时负责注销，"
          "WritePump 专职把 send 队列里的数据和定时 Ping 写回客户端，二者通过有界 channel 解耦。"),

    ("h2", "2.1 核心结构与代码骨架（要求白板可写）"),
    ("code", """type Client struct {
    conn *websocket.Conn
    room *Room
    send chan []byte   // 有界发送队列，如 make(chan []byte, 64)
    once sync.Once     // 保证多路径触发时注销只执行一次
}

const (
    writeWait  = 10 * time.Second // 单次写超时
    pongWait   = 60 * time.Second // 读死线：超过该时间没收到任何帧判定死连接
    pingPeriod = pongWait * 9 / 10 // 发送 Ping 的周期，必须小于 pongWait
)

// ReadPump：连接上唯一的读者
func (c *Client) readPump() {
    defer func() {
        c.room.remove(c) // 退出必经：从房间摘除并关闭 send
        c.conn.Close()
    }()
    c.conn.SetReadLimit(4096)
    c.conn.SetReadDeadline(time.Now().Add(pongWait))
    // 每收到一个 Pong 就把读死线向后顺延
    c.conn.SetPongHandler(func(string) error {
        return c.conn.SetReadDeadline(time.Now().Add(pongWait))
    })
    for {
        _, payload, err := c.conn.ReadMessage() // 阻塞读
        if err != nil {
            return // 出错/超时：defer 统一清理
        }
        c.room.onMessage(payload) // 校验 → 生成 msgID → 写 Kafka / 发 Redis
    }
}

// WritePump：连接上唯一的写者
func (c *Client) writePump() {
    ticker := time.NewTicker(pingPeriod)
    defer func() { ticker.Stop(); c.conn.Close() }()
    for {
        select {
        case msg, ok := <-c.send:
            c.conn.SetWriteDeadline(time.Now().Add(writeWait))
            if !ok { // send 已被房间关闭，回一个 Close 帧后退出
                c.conn.WriteMessage(websocket.CloseMessage, nil)
                return
            }
            if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
                return
            }
        case <-ticker.C: // 业务空闲也定时发 Ping 保活
            c.conn.SetWriteDeadline(time.Now().Add(writeWait))
            if c.conn.WriteMessage(websocket.PingMessage, nil) != nil {
                return
            }
        }
    }
}"""),
    ("note", ("关闭顺序（高频细节）：",
              "只能由房间的 remove() 在“持写锁把 client 从 map 删除之后”再 close(send)；"
              "WritePump 通过 channel 关闭（ok=false）或写失败退出。先摘 map 再 close，"
              "是为了保证此后不会再有广播向已关闭 channel 发送（向 closed channel 发送会直接 panic）。")),

    ("h2", "2.2 面试问答"),

    ("q", "Q5【做】★★ Client 结构体里有哪些字段？为什么发送数据要用 channel 而不是直接加锁写 conn？"),
    ("a", "字段至少包括底层 conn、所属房间、有界 send channel、保证注销一次的 sync.Once，"
          "可再带用户 ID 等。用 channel 的原因：①WebSocket 写不允许并发，多个来源（房间广播、人数通知、"
          "系统消息、Ping）都要写同一个连接，channel 天然把多路写串行化到 WritePump 单点；"
          "②有界 channel 同时充当发送缓冲区和背压窗口，队列长度可直接作为慢连接判据；"
          "③channel 满了可以用 select+default 做非阻塞丢弃/摘除，这是互斥锁方案很难表达的。"),

    ("q", "Q6【做】★★ 写一下 ReadPump 的主循环和它的 defer 清理逻辑。"),
    ("a", "见 2.1 代码。要点：defer 中做房间注销 + conn.Close，保证任何退出路径（读错误、超时、主动关闭）"
          "都能清理；循环体只做“读—处理”，处理逻辑上交房间，不在连接层做重活。"),

    ("q", "Q7【做】★★ WritePump 同时要发业务消息和定时 Ping，一个 select 怎么组织？"),
    ("a", "见 2.1：一个 for-select 两个 case——case <-c.send 写业务帧（并用 ok 判断 channel 是否被关闭），"
          "case <-ticker.C 写 Ping 帧；两条路径都先 SetWriteDeadline，任何写错误都 return，由 defer 关连接。"
          "Ping 周期必须略小于 Pong 死线（一般取死线的 90%），保证死线到期前一定发过一次探测。"),

    ("q", "Q8【做】★★ 心跳谁主动发？PongHandler 里为什么要 SetReadDeadline？"),
    ("a", "服务端 WritePump 定时主动发 Ping，浏览器/客户端协议栈自动回 Pong。SetReadDeadline 设了一个读死线："
          "只要连接健康，每次 Pong（或任何业务帧）到达都会在 PongHandler/读到数据时把死线顺延；"
          "一旦对端假死、再也不回任何帧，死线到期 ReadMessage 返回超时错误，ReadPump 退出并清理。"
          "它把“应用层心跳”和“死线检测”绑在了一起。"),

    ("q", "Q9【障】★★★ 用户直接拔网线/杀进程，服务端多久、靠什么感知？"),
    ("a", "不能指望 Read 立刻返回 error——拔网线后 TCP 没有机会发 FIN，服务端会一直阻塞在读上。"
          "感知靠两条：①Ping/Pong + 读死线，pongWait（如 60s）内收不到任何帧，ReadMessage 超时返回错误；"
          "②写时 TCP 重传/RST 最终失败，WritePump 写超时返回。所以最坏感知延迟约为一个心跳死线周期。"
          "这也是在线人数在异常断线后会短暂虚高、需要最终一致收敛的根本原因。"),

    ("q", "Q10【障】★★★ ReadPump 退出、慢连接摘除、心跳超时三条路径都可能触发注销，怎么保证只清理一次、不重复 close channel？"),
    ("a", "①remove() 内部用 sync.Once 包裹关闭动作，多次调用只有一次生效；②先 delete(map,c) 再 close(send)，"
          "delete 幂等，close 只执行一次；③广播侧持锁拷贝快照，摘除后新的广播不会再拿到该 client，"
          "杜绝向已关闭 channel 发送。要能说出“向已关闭 channel 发送会 panic、重复 close 也会 panic”，"
          "所以关闭权必须收敛到唯一位置。"),

    ("q", "Q11【因】★★ 为什么 WebSocket 的读和写都各自不允许并发？"),
    ("a", "gorilla/websocket 底层不保证一条连接上并发 ReadMessage / 并发 WriteMessage 的消息帧边界，"
          "并发写会导致控制帧（Ping/Pong/Close）和数据帧交错、帧损坏。因此约定“至多一个读者、至多一个写者”，"
          "这正是双协程模型存在的理由：读天然只有 ReadPump，写通过 channel 汇聚到 WritePump 单点。"),

    ("q", "Q12【放】★★ 单机 10 万长连接就是 20 万 goroutine，Go 凭什么扛得住？"),
    ("a", "三点：①goroutine 是用户态轻量线程，初始栈仅 2KB 且可动态增长，20 万 goroutine 的栈和调度成本远低于"
          "20 万内核线程（线程栈通常 8MB）；②Go runtime 自带 netpoller，网络连接在无数据时挂到 epoll（Linux），"
          "读阻塞的 goroutine 会被 park 而不是占住线程，数据就绪再唤醒，对应解决 C10K 问题；"
          "③GMP 调度让少量 M（线程）跑大量 G。真正的瓶颈通常是内存、fd 上限和广播扇出，而不是 goroutine 数量本身。"),

    ("q", "Q13【做】★ 单机连接数受什么系统限制？你调过哪些参数？"),
    ("a", "主要是：进程可打开文件描述符上限（ulimit -n，每个连接占一个 fd，需要调大到百万量级）；"
          "每连接内存（读缓冲、写缓冲、send 队列）×连接数；端口与连接跟踪表；以及 Go 层面 GOMAXPROCS。"
          "Linux 还要关注 somaxconn、tcp 内存、epoll 上限。能说出“fd 是第一道硬门槛”即可。"),
]
