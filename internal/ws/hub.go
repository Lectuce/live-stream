package ws

import (
	"context"
	"encoding/json"
	"live-stream/internal/model"
	"log"
	"runtime"
	"sync"
)

type Room struct {
	clients     map[*Client]bool  // 房间内的连接集合
	onlineUsers map[uint64]uint64 // 房间内的用户在线计数（同账号多端累加）
	mu          sync.RWMutex      // 读房间级读写锁，仅保护本房间数据
}

type Hub struct {
	rooms          map[uint64]*Room // 全局房间字典， 房间ID -> 房间实例
	broadcastTasks chan Message     // 需要广播的弹幕, 广播任务队列，多worker消费
	register       chan *Client     // 新连接入房
	unregister     chan *Client     // 连接退出
	mu             sync.Mutex       // 全局锁，仅保护rooms字典的增删
	ctx            context.Context
	cancel         context.CancelFunc
	workerNum      int
}

func NewHub() *Hub {
	worknum := runtime.NumCPU() * 2
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		rooms:          make(map[uint64]*Room),
		broadcastTasks: make(chan Message, 4096),
		register:       make(chan *Client, 512),
		unregister:     make(chan *Client, 512),
		ctx:            ctx,
		cancel:         cancel,
		workerNum:      worknum,
	}
}

func (h *Hub) Stop() {
	h.cancel()
}

func (h *Hub) broadcastWorker() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case msg := <-h.broadcastTasks:
			h.broadcastToRoom(&msg)
		}
	}
}

func (h *Hub) Run() {
	for i := 0; i < h.workerNum; i++ {
		go h.broadcastWorker()
	}

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
	}
	onlineCount := len(room.clients)
	room.mu.Unlock()
	h.mu.Unlock()

	msg := model.Message{
		Type:   model.MsgTypeSystem,
		RoomID: client.RoomID,
		Data: map[string]interface{}{
			"sub_type":     "online_count",
			"online_count": onlineCount,
		},
	}
	select {
	case h.broadcastTasks <- msg:
	default:
	}

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
	room.onlineUsers[client.UserID]--
	if room.onlineUsers[client.UserID] <= 0 {
		delete(room.onlineUsers, client.UserID)
	}
	clientCount := len(room.clients)
	onlineCount := clientCount
	client.closeSend()
	if clientCount == 0 {
		delete(h.rooms, client.RoomID)
	}
	room.mu.Unlock()
	h.mu.Unlock()

	msg := model.Message{
		Type:   model.MsgTypeSystem,
		RoomID: client.RoomID,
		Data: map[string]interface{}{
			"sub_type":     "online_count",
			"online_count": onlineCount,
		},
	}
	select {
	case h.broadcastTasks <- msg:
	default:
	}

}

func (h *Hub) broadcastToRoom(msg *Message) {
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

	for _, cli := range clients {
		if !cli.trySend(data) {
			log.Printf("client send channel full, drop client room=%d user=%d", cli.RoomID, cli.UserID)
			cli.signalUnregister()
		}
	}
}
