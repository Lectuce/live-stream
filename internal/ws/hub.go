package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"live-stream/internal/model"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Room struct {
	snapshotCounter *atomic.Uint64
	shardSnapshots  [fanoutShardCount][]*Client
	shardDirty      [fanoutShardCount]bool
	clients         map[*Client]bool // 房间内的连接集合
	shards          [fanoutShardCount]map[*Client]bool
	onlineUsers     map[uint64]uint64 // 房间内的用户在线计数（同账号多端累加）
	likeCount       int64
	policy          roomAdaptiveState
	mu              sync.RWMutex // 读房间级读写锁，仅保护本房间数据
}

const fanoutShardCount = 16

type fanoutJob struct {
	room     *Room
	queuedAt time.Time
	roomID   uint64
	shard    int
	frame    *websocket.PreparedMessage
	roomSeq  uint64
	hot      bool
}

type Hub struct {
	workers         sync.WaitGroup
	closeQueue      chan closeRequest
	rooms           map[uint64]*Room // 全局房间字典， 房间ID -> 房间实例
	broadcastQueues []chan fanoutJob // 同一房间的客户端分片映射到固定 worker
	register        chan *Client     // 新连接入房
	unregister      chan *Client     // 连接退出
	mu              sync.Mutex       // 全局锁，仅保护rooms字典的增删
	ctx             context.Context
	cancel          context.CancelFunc
	workerNum       int
	presenceMu      sync.Mutex
	presenceDirty   map[uint64]struct{}
	presencePeriod  time.Duration
	adaptive        adaptiveSettings
	metrics         hubMetrics
}

func NewHub() *Hub {
	return newHub(runtime.NumCPU() * 2)
}

func newHub(workerNum int) *Hub {
	if workerNum < 1 {
		workerNum = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	queues := make([]chan fanoutJob, workerNum)
	for i := range queues {
		queues[i] = make(chan fanoutJob, 4096)
	}
	return &Hub{
		rooms:           make(map[uint64]*Room),
		closeQueue:      make(chan closeRequest, 256),
		broadcastQueues: queues,
		register:        make(chan *Client, 512),
		unregister:      make(chan *Client, 512),
		ctx:             ctx,
		cancel:          cancel,
		workerNum:       workerNum,
		presenceDirty:   make(map[uint64]struct{}),
		presencePeriod:  250 * time.Millisecond,
		adaptive:        loadAdaptiveSettings(),
	}
}

func (h *Hub) Stop() {
	h.mu.Lock()
	h.cancel()
	h.mu.Unlock()
	h.workers.Wait()
	h.cleanupClients()
}

// SnapshotOnlineCounts 返回此网关中每个本地活跃房间的当前连接数。
// 热度排行协程会将该快照作为实例级短期在线心跳发布到 Redis。
func (h *Hub) SnapshotOnlineCounts() map[uint64]int {
	h.mu.Lock()
	rooms := make(map[uint64]*Room, len(h.rooms))
	for roomID, room := range h.rooms {
		rooms[roomID] = room
	}
	h.mu.Unlock()

	counts := make(map[uint64]int, len(rooms))
	for roomID, room := range rooms {
		room.mu.RLock()
		counts[roomID] = len(room.clients)
		room.mu.RUnlock()
	}
	return counts
}

func (h *Hub) broadcastWorker(queue <-chan fanoutJob) {
	for {
		select {
		case <-h.ctx.Done():
			return
		case job := <-queue:
			startedAt := time.Now()
			h.metrics.dispatchWait.observe(time.Since(job.queuedAt))
			h.broadcastRoomShard(job.room, job.shard, job.frame, job.roomSeq, job.hot)
			h.metrics.fanoutShardDuration.observe(time.Since(startedAt))
		}
	}
}

func (h *Hub) enqueueBroadcast(msg Message) {
	startedAt := time.Now()
	deliver, changedPolicy := h.planDanmaDelivery(msg, startedAt)
	if changedPolicy != nil {
		h.broadcastDeliveryPolicy(*changedPolicy)
	}
	if !deliver {
		h.metrics.sampledMessagesTotal.Add(1)
		return
	}
	frame, err := prepareMessage(&msg)
	if err != nil {
		log.Printf("prepare websocket broadcast failed: %v", err)
		return
	}
	h.metrics.broadcastMessagesTotal.Add(1)
	defer func() {
		h.metrics.broadcastDuration.observe(time.Since(startedAt))
	}()

	// 房间按连接而不是按消息分片。每个“房间+分片”组合始终由同一个
	// FIFO worker 消费，因此同一客户端不会遇到两个 worker 竞争入队
	// 不同房间消息的情况。
	hot := h.roomIsHot(msg.RoomID)
	room := h.lookupRoom(msg.RoomID)
	if room == nil {
		return
	}
	for shard := 0; shard < fanoutShardCount; shard++ {
		queue := h.broadcastQueues[h.broadcastQueueIndex(msg.RoomID, shard)]
		queuedAt := time.Now()
		select {
		case queue <- fanoutJob{
			room: room, queuedAt: queuedAt,
			roomID:  msg.RoomID,
			shard:   shard,
			frame:   frame,
			roomSeq: msg.RoomSeq,
			hot:     hot,
		}:
			h.metrics.enqueueWait.observe(time.Since(queuedAt))
		case <-h.ctx.Done():
			return
		}
	}
}

func (h *Hub) broadcastQueueIndex(roomID uint64, shard int) int {
	return int((roomID*fanoutShardCount + uint64(shard)) % uint64(len(h.broadcastQueues)))
}

func (h *Hub) Run() {
	h.mu.Lock()
	if h.ctx.Err() != nil {
		h.mu.Unlock()
		return
	}
	h.workers.Add(1)
	start := func(fn func()) { h.workers.Add(1); go func() { defer h.workers.Done(); fn() }() }
	start(h.closeWorker)
	start(h.closeWorker)
	start(h.adaptiveWorker)
	for _, queue := range h.broadcastQueues {
		q := queue
		start(func() { h.broadcastWorker(q) })
	}
	start(h.presenceWorker)
	start(h.metricsWorker)
	h.mu.Unlock()
	defer h.workers.Done()
	defer h.cleanupClients()
	log.Printf("hub started, workerNum=%d", h.workerNum)
	for {
		select {
		case <-h.ctx.Done():
			log.Println("hub exit signal received, stop run")
			return
		case client := <-h.register:
			h.handleRegister(client)
		case client := <-h.unregister:
			h.handleUnregister(client)
		}
	}

}

func (h *Hub) handleRegister(client *Client) {
	h.mu.Lock()
	if h.ctx.Err() != nil {
		h.mu.Unlock()
		client.closeConnection()
		client.closeSend()
		return
	}
	room, ok := h.rooms[client.RoomID]
	// 如果房间不存在该客户端，则初始化
	if !ok {
		room = &Room{
			snapshotCounter: &h.metrics.snapshotRebuilds,
			clients:         make(map[*Client]bool),
			onlineUsers:     make(map[uint64]uint64),
			likeCount:       client.initialLikeCount,
		}
		for shard := range room.shards {
			room.shards[shard] = make(map[*Client]bool)
		}
		h.rooms[client.RoomID] = room
	}
	room.mu.Lock()
	var currentPolicy *deliveryPolicySnapshot
	if _, exists := room.clients[client]; !exists {
		room.clients[client] = true
		room.shards[client.fanoutShard][client] = true
		room.shardDirty[client.fanoutShard] = true
		room.onlineUsers[client.UserID]++
		h.metrics.connections.Add(1)
		if room.policy.sampleStride > 1 {
			currentPolicy = &deliveryPolicySnapshot{
				roomID:       client.RoomID,
				hot:          room.policy.hot,
				sourceQPS:    room.policy.sourceQPS,
				fanoutQPS:    room.policy.fanoutQPS,
				sampleStride: room.policy.sampleStride,
				latestSeq:    room.policy.latestSeq,
				replayAfter:  room.policy.replayAfter,
			}
		}
	}
	room.mu.Unlock()
	h.mu.Unlock()
	if currentPolicy != nil {
		h.enqueueDeliveryPolicy(client, *currentPolicy)
	}
	h.markPresenceDirty(client.RoomID)
}

func (h *Hub) handleUnregister(client *Client) {
	h.mu.Lock()
	room, ok := h.rooms[client.RoomID]
	if !ok {
		h.mu.Unlock()
		client.closeSend()
		return
	}

	room.mu.Lock()
	if _, exists := room.clients[client]; !exists {
		room.mu.Unlock()
		h.mu.Unlock()
		client.closeSend()
		return
	}
	delete(room.clients, client)
	delete(room.shards[client.fanoutShard], client)
	room.shardDirty[client.fanoutShard] = true
	h.metrics.connections.Add(-1)
	h.metrics.closedClientsTotal.Add(1)
	room.onlineUsers[client.UserID]--
	if room.onlineUsers[client.UserID] <= 0 {
		delete(room.onlineUsers, client.UserID)
	}
	clientCount := len(room.clients)
	client.closeSend()
	if clientCount == 0 {
		delete(h.rooms, client.RoomID)
	}
	room.mu.Unlock()
	h.mu.Unlock()
	h.markPresenceDirty(client.RoomID)
}

// markPresenceDirty 仅保存每个房间最新的待处理状态。因此，短时间内集中发生的
// 加入/离开事件在每个周期只会产生一次在线状态更新，而不会为每次连接事件
// 都触发一次全房间广播。
func (h *Hub) markPresenceDirty(roomID uint64) {
	h.presenceMu.Lock()
	h.presenceDirty[roomID] = struct{}{}
	h.presenceMu.Unlock()
}

func (h *Hub) takeDirtyRooms() []uint64 {
	h.presenceMu.Lock()
	roomIDs := make([]uint64, 0, len(h.presenceDirty))
	for roomID := range h.presenceDirty {
		roomIDs = append(roomIDs, roomID)
		delete(h.presenceDirty, roomID)
	}
	h.presenceMu.Unlock()
	return roomIDs
}

func (h *Hub) presenceWorker() {
	ticker := time.NewTicker(h.presencePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			for _, roomID := range h.takeDirtyRooms() {
				if h.presenceDue(roomID, time.Now()) {
					h.broadcastOnlineCount(roomID)
				} else {
					h.markPresenceDirty(roomID)
				}
			}
		}
	}
}

func (h *Hub) broadcastOnlineCount(roomID uint64) {
	h.mu.Lock()
	room, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	room.mu.RLock()
	h.mu.Unlock()
	onlineCount := len(room.clients)
	likeCount := room.likeCount
	room.mu.RUnlock()

	msg := model.Message{
		Type:   model.MsgTypeSystem,
		RoomID: roomID,
		Data: map[string]interface{}{
			"sub_type":     "room_state",
			"online_count": onlineCount,
			"like_count":   likeCount,
		},
	}
	// 在线状态属于可覆盖的状态更新，因此特意不放入弹幕使用的事件队列。
	h.broadcastStateToRoom(&msg)
}

func (h *Hub) handleStateUpdate(msg *Message) {
	if msg.Data == nil {
		return
	}

	subType, _ := msg.Data["sub_type"].(string)
	switch subType {
	case "like_count":
		likeCount, ok := msg.Data["like_count"].(float64)
		if !ok {
			return
		}
		h.mu.Lock()
		room, exists := h.rooms[msg.RoomID]
		if !exists {
			h.mu.Unlock()
			return
		}
		room.mu.Lock()
		h.mu.Unlock()
		room.likeCount = int64(likeCount)
		room.mu.Unlock()
		h.markPresenceDirty(msg.RoomID)
	case "room_status":
		h.broadcastControlToRoom(msg)

	}

}

func (h *Hub) broadcastStateToRoom(msg *Message) {
	h.metrics.stateBroadcasts.Add(1)
	startedAt := time.Now()
	h.metrics.broadcastMessagesTotal.Add(1)
	defer func() {
		h.metrics.broadcastDuration.observe(time.Since(startedAt))
	}()
	frame, err := prepareMessage(msg)
	if err != nil {
		log.Printf("prepare websocket state broadcast failed: %v", err)
		return
	}
	for shard := 0; shard < fanoutShardCount; shard++ {
		h.mu.Lock()
		room, ok := h.rooms[msg.RoomID]
		if !ok {
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
		clients := room.snapshotShard(shard)
		for _, client := range clients {
			if client.replaceState(frame) == enqueueOK {
				h.metrics.deliveryEnqueuedTotal.Add(1)
			}
		}
	}
}

func (h *Hub) broadcastControlToRoom(msg *Message) {
	frame, err := prepareMessage(msg)
	if err != nil {
		log.Printf("prepare websocket control broadcast failed: %v", err)
		return
	}
	for shard := 0; shard < fanoutShardCount; shard++ {
		h.mu.Lock()
		room := h.rooms[msg.RoomID]
		if room == nil {
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
		clients := room.snapshotShard(shard)
		for _, client := range clients {
			if result := client.trySendControl(frame); result == enqueueFull {
				h.metrics.controlQueueFullTotal.Add(1)
				h.requestClientClose(client, 4001, "control_queue_full")
			}
		}
	}
}

func (h *Hub) broadcastToRoom(msg *Message) {
	startedAt := time.Now()
	h.metrics.broadcastMessagesTotal.Add(1)
	defer func() {
		h.metrics.broadcastDuration.observe(time.Since(startedAt))
	}()

	frame, err := prepareMessage(msg)
	if err != nil {
		log.Printf("prepare websocket broadcast failed: %v", err)
		return
	}
	for shard := 0; shard < fanoutShardCount; shard++ {
		h.broadcastToShard(msg.RoomID, shard, frame, msg.RoomSeq, h.roomIsHot(msg.RoomID))
	}
}

func prepareMessage(value any) (*websocket.PreparedMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return websocket.NewPreparedMessage(websocket.TextMessage, data)
}

func (h *Hub) broadcastToShard(
	roomID uint64,
	shard int,
	frame *websocket.PreparedMessage,
	roomSeq uint64,
	hot bool,
) {
	h.mu.Lock()
	room, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.broadcastRoomShard(room, shard, frame, roomSeq, hot)
}
func (h *Hub) broadcastRoomShard(room *Room, shard int, frame *websocket.PreparedMessage, roomSeq uint64, hot bool) {
	if room == nil {
		return
	}
	clients := room.snapshotShard(shard)

	for _, client := range clients {
		h.enqueueClient(client, frame, roomSeq, hot)
	}
}

func (h *Hub) enqueueJSON(client *Client, value any) {
	frame, err := prepareMessage(value)
	if err != nil {
		log.Printf("prepare websocket response failed: %v", err)
		return
	}
	if result := client.trySendControl(frame); result == enqueueFull {
		h.metrics.controlQueueFullTotal.Add(1)
		h.requestClientClose(client, 4001, "control_queue_full")
	}
}

func (h *Hub) resyncFrame(client *Client, latestSeq uint64) *websocket.PreparedMessage {
	frame, err := prepareMessage(map[string]any{
		"type":       "resync_required",
		"room_id":    client.RoomID,
		"reason":     "live_queue_gap",
		"after_seq":  client.lastWrittenSeq.Load(),
		"latest_seq": latestSeq,
		"replay_url": fmt.Sprintf("/api/v1/rooms/%d/messages/replay", client.RoomID),
	})
	if err != nil {
		log.Printf("prepare resync control message failed: %v", err)
		return nil
	}
	return frame
}

func (h *Hub) enqueueClient(
	client *Client,
	message *websocket.PreparedMessage,
	roomSeq uint64,
	hot bool,
) {
	result := client.trySendData(
		message,
		roomSeq,
		hot,
		h.adaptive.queueHighWatermark,
	)
	switch result {
	case enqueueOK:
		h.metrics.deliveryEnqueuedTotal.Add(1)
	case enqueueClosed:
		// 注销流程已经负责清理已关闭的队列。
	case enqueueDroppedOldest, enqueueFull:
		h.metrics.queueFullTotal.Add(1)
		if result == enqueueDroppedOldest {
			h.metrics.droppedOldestTotal.Add(1)
			h.metrics.deliveryEnqueuedTotal.Add(1)
		}
		notifyGap, disconnect := client.recordQueuePressure(
			time.Now(),
			h.adaptive.slowStrikeLimit,
			h.adaptive.slowGrace,
		)
		if notifyGap {
			h.metrics.resyncRequiredTotal.Add(1)
			if frame := h.resyncFrame(client, roomSeq); frame != nil {
				if result := client.trySendControl(frame); result == enqueueFull {
					h.metrics.controlQueueFullTotal.Add(1)
					disconnect = true
				}
			}
		}
		if disconnect {
			h.metrics.slowClientDisconnectTotal.Add(1)
			h.requestClientClose(client, 4001, "resync_required")
		}
	}
}
