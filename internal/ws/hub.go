package ws

import (
	"context"
	"encoding/json"
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

var bufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 512)
	},
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
	h.mu.Unlock()

	// 标记为在线（true）
	h.mu.Lock()
	room.clients[client] = true
	room.onlineUsers[client.UserID]++
	h.mu.Unlock()

}

func (h *Hub) handleUnregister(client *Client) {
	// 全局锁：仅取房间指针 + 后续空房间删除
	h.mu.Lock()
	room, ok := h.rooms[client.RoomID]
	h.mu.Unlock()

	if !ok {
		h.mu.Unlock()
		return
	}
	// 房间级写锁：只操作当前房间的客户端集合
	room.mu.Lock()
	delete(room.clients, client)
	room.onlineUsers[client.UserID]--
	if room.onlineUsers[client.UserID] <= 0 {
		delete(room.onlineUsers, client.UserID)
	}
	clientCount := len(room.clients)
	room.mu.Unlock()

	// 房间空了，全局锁删除房间条目
	if clientCount == 0 {
		h.mu.Lock()
		delete(h.rooms, client.RoomID)
		h.mu.Unlock()
	}

}

func (h *Hub) broadcastToRoom(msg *Message) {
	h.mu.Lock()
	room, ok := h.rooms[msg.RoomID]
	h.mu.Unlock()
	if !ok {
		return
	}
	room.mu.RLock()
	clients := make([]*Client, 0, len(room.clients))
	for c := range room.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	// 从池里取缓冲区
	buf := bufPool.Get().([]byte)
	// 序列化到复用的缓冲区
	buf = buf[:0] // 重置长度，底层数组复用
	marshalBuf, err := json.Marshal(msg)
	if err == nil {
		buf = append(buf, marshalBuf...)
	}

	data, _ := json.Marshal(msg)
	for cli := range room.clients {
		select {
		case cli.send <- data:
		default:
			log.Printf("client send channel full, drop client room=%d user=%d", cli.RoomID, cli.UserID)
			h.unregister <- cli
		}
	}
	bufPool.Put(buf)
}
