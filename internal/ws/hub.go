package ws

import (
	"context"
	"encoding/json"
	"live-stream/internal/model"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Room struct {
	clients     map[*Client]bool  // 房间内的连接集合
	onlineUsers map[uint64]uint64 // 房间内的用户在线计数（同账号多端累加）
	mu          sync.RWMutex      // 读房间级读写锁，仅保护本房间数据
}

type Hub struct {
	rooms           map[uint64]*Room // 全局房间字典， 房间ID -> 房间实例
	broadcastQueues []chan Message   // 按房间分片的广播队列，每个分片由一个 worker 消费
	register        chan *Client     // 新连接入房
	unregister      chan *Client     // 连接退出
	mu              sync.Mutex       // 全局锁，仅保护rooms字典的增删
	ctx             context.Context
	cancel          context.CancelFunc
	workerNum       int
	presenceMu      sync.Mutex
	presenceDirty   map[uint64]struct{}
	presencePeriod  time.Duration
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
	queues := make([]chan Message, workerNum)
	for i := range queues {
		queues[i] = make(chan Message, 4096)
	}
	return &Hub{
		rooms:           make(map[uint64]*Room),
		broadcastQueues: queues,
		register:        make(chan *Client, 512),
		unregister:      make(chan *Client, 512),
		ctx:             ctx,
		cancel:          cancel,
		workerNum:       workerNum,
		presenceDirty:   make(map[uint64]struct{}),
		presencePeriod:  250 * time.Millisecond,
	}
}

func (h *Hub) Stop() {
	h.cancel()
}

func (h *Hub) broadcastWorker(queue <-chan Message) {
	for {
		select {
		case <-h.ctx.Done():
			return
		case msg := <-queue:
			h.broadcastToRoom(&msg)
		}
	}
}

func (h *Hub) enqueueBroadcast(msg Message) {
	queue := h.broadcastQueues[h.broadcastQueueIndex(msg.RoomID)]
	select {
	case queue <- msg:
	case <-h.ctx.Done():
	}
}

func (h *Hub) broadcastQueueIndex(roomID uint64) int {
	return int(roomID % uint64(len(h.broadcastQueues)))
}

func (h *Hub) Run() {
	for _, queue := range h.broadcastQueues {
		go h.broadcastWorker(queue)
	}
	go h.presenceWorker()
	go h.metricsWorker()

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
	room, ok := h.rooms[client.RoomID]
	// 如果房间不存在该客户端，则初始化
	if !ok {
		room = &Room{
			clients:     make(map[*Client]bool),
			onlineUsers: make(map[uint64]uint64),
		}
		h.rooms[client.RoomID] = room
	}
	room.mu.Lock()
	if _, exists := room.clients[client]; !exists {
		room.clients[client] = true
		room.onlineUsers[client.UserID]++
		h.metrics.connections.Add(1)
	}
	room.mu.Unlock()
	h.mu.Unlock()
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

// markPresenceDirty stores only the latest pending state for each room. A burst
// of joins/leaves therefore creates one presence update per period, not one
// room-wide broadcast per connection event.
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
				h.broadcastOnlineCount(roomID)
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
	room.mu.RUnlock()

	msg := model.Message{
		Type:   model.MsgTypeSystem,
		RoomID: roomID,
		Data: map[string]interface{}{
			"sub_type":     "online_count",
			"online_count": onlineCount,
		},
	}
	// Presence is a replaceable state update, so it is deliberately kept out
	// of the event queue used by danmaku messages.
	h.broadcastToRoom(&msg)
}

func (h *Hub) broadcastToRoom(msg *Message) {
	startedAt := time.Now()
	h.metrics.broadcastMessagesTotal.Add(1)
	defer func() {
		h.metrics.broadcastDuration.observe(time.Since(startedAt))
	}()

	h.mu.Lock()
	room, ok := h.rooms[msg.RoomID]
	if !ok {
		h.mu.Unlock()
		return
	}
	room.mu.RLock()
	h.mu.Unlock()
	clients := make([]*Client, 0, len(room.clients))
	for c := range room.clients {
		clients = append(clients, c)
	}
	room.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal websocket broadcast failed: %v", err)
		return
	}
	frame, err := websocket.NewPreparedMessage(websocket.TextMessage, data)
	if err != nil {
		log.Printf("prepare websocket broadcast failed: %v", err)
		return
	}
	for _, cli := range clients {
		h.enqueueClient(cli, frame)
	}
}

func (h *Hub) enqueueJSON(client *Client, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("marshal websocket response failed: %v", err)
		return
	}
	frame, err := websocket.NewPreparedMessage(websocket.TextMessage, data)
	if err != nil {
		log.Printf("prepare websocket response failed: %v", err)
		return
	}
	h.enqueueClient(client, frame)
}

func (h *Hub) enqueueClient(client *Client, message *websocket.PreparedMessage) {
	switch client.trySend(message) {
	case enqueueOK:
		h.metrics.deliveryEnqueuedTotal.Add(1)
	case enqueueClosed:
		// The unregister path already owns cleanup for a closed queue.
	case enqueueFull:
		h.metrics.queueFullTotal.Add(1)
		client.closeConnectionWithReason(4001, "resync_required")
		client.signalUnregister()
	}
}
