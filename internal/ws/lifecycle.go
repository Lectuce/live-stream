package ws

import (
	"github.com/gorilla/websocket"
	"time"
)

type closeRequest struct {
	client *Client
	code   int
	reason string
}

func (h *Hub) requestClientClose(c *Client, code int, reason string) {
	if h.ctx.Err() != nil {
		c.closeConnection()
		return
	}
	if !c.closing.CompareAndSwap(false, true) {
		return
	}
	select {
	case h.closeQueue <- closeRequest{c, code, reason}:
		h.metrics.closeQueued.Add(1)
	default:
		h.metrics.closeOverflow.Add(1)
		c.closeConnection()
	}
}

func (h *Hub) closeWorker() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case request := <-h.closeQueue:
			c := request.client
			if c.conn != nil {
				_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(request.code, request.reason), time.Now().Add(100*time.Millisecond))
			}
			c.closeConnection()
			c.signalUnregister()
		}
	}
}

func (h *Hub) cleanupClients() {
	h.mu.Lock()
	var clients []*Client
	for _, room := range h.rooms {
		room.mu.RLock()
		for c := range room.clients {
			clients = append(clients, c)
		}
		room.mu.RUnlock()
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.closeConnection()
		h.handleUnregister(c)
	}
	for {
		select {
		case c := <-h.register:
			c.closeConnection()
			c.closeSend()
		default:
			return
		}
	}
}

func (h *Hub) lookupRoom(id uint64) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[id]
}

// Published snapshots are immutable; callers may retain them after unlocking.
func (r *Room) snapshotShard(shard int) []*Client {
	r.mu.RLock()
	if !r.shardDirty[shard] {
		result := r.shardSnapshots[shard]
		r.mu.RUnlock()
		return result
	}
	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shardDirty[shard] {
		next := make([]*Client, 0, len(r.shards[shard]))
		for c := range r.shards[shard] {
			next = append(next, c)
		}
		r.shardSnapshots[shard] = next
		r.shardDirty[shard] = false
		if r.snapshotCounter != nil {
			r.snapshotCounter.Add(1)
		}
	}
	return r.shardSnapshots[shard]
}
