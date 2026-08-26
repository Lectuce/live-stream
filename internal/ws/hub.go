package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
)

type Hub struct {
	rooms           map[int64]map[*Client]bool // 房间内的在线客户端
	roomOnlineUsers map[int64]map[int64]int64
	Broadcast       chan Message // 需要广播的弹幕
	register        chan *Client // 新连接入房
	unregister      chan *Client // 连接退出
	mu              sync.Mutex   // 锁
	ctx             context.Context
	cancel          context.CancelFunc
}

func NewHub() *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		rooms:           make(map[int64]map[*Client]bool),
		roomOnlineUsers: make(map[int64]map[int64]int64),
		Broadcast:       make(chan Message, 1024),
		register:        make(chan *Client),
		unregister:      make(chan *Client),
		ctx:             ctx,
		cancel:          cancel,
	}
}

func (h *Hub) Stop() {
	h.cancel()
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.ctx.Done():
			log.Println("hub exit signal received, stop run")
			return
		case client := <-h.register:
			h.mu.Lock()
			_, ok1 := h.rooms[client.RoomID]
			_, ok2 := h.roomOnlineUsers[client.RoomID]
			// 如果房间不存在该客户端，则初始化
			if !ok1 {
				h.rooms[client.RoomID] = make(map[*Client]bool)
			}
			if !ok2 {
				h.roomOnlineUsers[client.RoomID] = make(map[int64]int64)
			}
			// 标记为在线（true）
			h.rooms[client.RoomID][client] = true
			h.roomOnlineUsers[client.RoomID][client.UserID]++
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			room, ok1 := h.rooms[client.RoomID]
			roomuser, ok2 := h.roomOnlineUsers[client.RoomID]
			if ok1 {
				delete(room, client)
				if len(room) == 0 {
					delete(h.rooms, client.RoomID)
				}
			}
			if ok2 {
				roomuser[client.UserID]--
				if roomuser[client.UserID] <= 0 {
					delete(roomuser, client.UserID)
					if len(roomuser) == 0 {
						delete(h.roomOnlineUsers, client.RoomID)
					}
				}

			}
			h.mu.Unlock()
		case msg := <-h.Broadcast:
			h.mu.Lock()
			roomClients, ok := h.rooms[msg.RoomID]
			if !ok {
				h.mu.Unlock()
				continue
			}
			clients := make([]*Client, 0, len(roomClients))
			for c := range roomClients {
				clients = append(clients, c)
			}
			h.mu.Unlock()
			data, _ := json.Marshal(msg)
			for cli := range roomClients {
				select {
				case cli.send <- data:
				default:
					// close(cli.send)
					log.Printf("client send channel full, drop client room=%d user=%d", cli.RoomID, cli.UserID)
					// delete(roomClients, cli)
					h.unregister <- cli
				}
			}

		}
	}
}
