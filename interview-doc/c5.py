# -*- coding: utf-8 -*-
"""第 5 章 在线人数合并定时广播"""

BLOCKS = [
    ("h1", "五、在线人数：按房间合并、定时广播、多节点聚合"),
    ("p", "热门直播间进出非常频繁，如果每次进房/退房都向全房间广播一次人数，会产生大量重复、抖动的广播，"
          "且多节点下每个节点只知道本机连接数，必须聚合。方案是“本地只标脏 + 定时合并 flush + Redis 分节点计数”。"),

    ("h2", "5.1 合并广播与多节点聚合实现"),
    ("code", """type OnlineCounter struct {
    mu    sync.Mutex
    dirty map[string]struct{} // 当前合并窗口内人数发生变化的房间
    local map[string]int      // roomID -> 本节点连接数
}

// 进/退房：只改本地计数并“标脏”，不立即广播
func (o *OnlineCounter) mark(roomID string, delta int) {
    o.mu.Lock()
    o.local[roomID] += delta
    o.dirty[roomID] = struct{}{}
    o.mu.Unlock()
}

// 单独一个 ticker goroutine，每个合并窗口（如 1s）flush 一次
go func() {
    ticker := time.NewTicker(time.Second)
    for range ticker.C {
        for _, roomID := range o.drainDirty() { // 取出并清空脏集合
            n := o.localCount(roomID)
            // 分节点计数：field=节点ID，value=该节点人数；定时覆盖+续期
            key := "online:" + roomID
            rdb.HSet(ctx, key, nodeID, n)
            rdb.Expire(ctx, key, 10*time.Second) // 节点宕机后其 field 随 TTL 过期
            m, _ := rdb.HGetAll(ctx, key).Result()
            total := 0
            for _, v := range m { total += atoi(v) } // 汇总各节点
            hub.dispatchToRoom(roomID, encodeOnline(total))
        }
    }
}()"""),

    ("h2", "5.2 面试问答"),

    ("q", "Q33【做】★★ “按房间合并、定时广播”在代码里怎么落地？"),
    ("a", "维护一个 dirty 集合：进房/退房只更新本地计数并把 roomID 标记为脏，不触发广播；"
          "单独一个 ticker goroutine 每固定窗口（如 1 秒）取出本窗口所有脏房间，每个房间只计算、广播一次最新人数"
          "（见 5.1）。这样无论窗口内进出抖动多少次，对外只有一次广播，把 O(进出次数) 次广播压成 O(变化房间数) 次。"),

    ("q", "Q34【做】★★★ 多节点人数怎么聚合？为什么用 Hash（field=节点）而不是全局 INCR/DECR？"),
    ("akey", "全局 INCR/DECR 的致命问题是无法处理节点异常：",
          "某节点宕机时它持有的连接没有机会执行 DECR，全局计数永久偏大且无法自愈。改用 Hash："
          "key 为房间、field 为节点 ID、value 为该节点当前人数，由各节点定时 HSet 覆盖自己的 field 并刷新 TTL，"
          "房间总人数 = HGetAll 后对所有 field 求和。节点宕机后停止续期，其 field 随 key/field 过期机制被清理，"
          "计数自动收敛，把“增量记账”变成“定期上报现状”，天然容错。"),

    ("q", "Q35【障】★★★ 某节点宕机，它贡献的人数怎么清掉？"),
    ("a", "靠 TTL：节点定时（如每秒）HSet 并 Expire 续期（TTL 设为几个上报周期，如 10 秒）；"
          "节点宕机后不再续期，key 或其 field 到期被删除，汇总时不再计入。"
          "因此从宕机到人数收敛最坏延迟约为一个 TTL，这是用最终一致换取实现简单和容错，"
          "比依赖每连接精确 DECR 更健壮（后者在 kill -9、断电时必然漏减）。"),

    ("q", "Q36【障】★★ 断线但服务端尚未感知的窗口期人数虚高，怎么收敛？承诺强一致吗？"),
    ("a", "异常断线要等心跳死线（pongWait）才被 ReadPump 判定并触发本地计数扣减，叠加多节点 TTL，"
          "在线人数是最终一致而非实时精确，短暂虚高是正常的。收敛路径：死线检测→本地计数修正并标脏→"
          "下个合并窗口 HSet 覆盖→汇总值下降。面试时主动声明“在线人数做最终一致、不做强一致”，"
          "并说明业务上人数展示容忍秒级误差。"),

    ("q", "Q37【因】★★ 合并窗口定多大？省广播量和人数实时性怎么权衡？"),
    ("a", "窗口越大，抖动合并越充分、广播和 Redis 操作越省，但用户看到的人数延迟越大；窗口越小越实时但越接近“每次进出都广播”。"
          "一般取 1 秒左右：人眼对在线人数秒级变化不敏感，却能把热门房间每秒成百上千次进出合并成一次广播。"
          "具体值按进出峰值 QPS 和可接受展示延迟确定，属于可配置参数。"),
]
