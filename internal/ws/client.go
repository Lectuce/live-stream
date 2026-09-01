package ws

import (
	"live-stream/internal/kafka"
	"live-stream/internal/model"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/gorilla/websocket"
)

const writeWait = 5 * time.Second

type enqueueResult uint8

const (
	enqueueOK enqueueResult = iota
	enqueueClosed
	enqueueFull
)

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	RoomID   uint64                          // 当前客户端所在的房间
	send     chan *websocket.PreparedMessage // 下发给前端的消息队列
	UserID   uint64                          // 当前客户端的用户id
	UserName string

	sendMu         sync.RWMutex
	sendClosed     bool
	unregisterOnce sync.Once
	closeOnce      sync.Once
	closing        atomic.Bool
	lastAck        atomic.Uint64
}

func NewClient(hub *Hub, conn *websocket.Conn, roomID uint64, userID uint64, userName string) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		RoomID:   roomID,
		UserID:   userID,
		UserName: userName,
		send:     make(chan *websocket.PreparedMessage, 256),
	}
}

func (c *Client) trySend(message *websocket.PreparedMessage) enqueueResult {
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.sendClosed {
		return enqueueClosed
	}
	select {
	case c.send <- message:
		return enqueueOK
	default:
		return enqueueFull
	}
}

func (c *Client) closeSend() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendClosed {
		return
	}
	c.sendClosed = true
	close(c.send)
}

func (c *Client) signalUnregister() {
	c.unregisterOnce.Do(func() {
		select {
		case c.hub.unregister <- c:
		case <-c.hub.ctx.Done():
		}
	})
}

func (c *Client) closeConnection() {
	c.closeConnectionWithReason(0, "")
}

func (c *Client) closeConnectionWithReason(code int, reason string) {
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		if c.conn != nil {
			if code != 0 {
				_ = c.conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(code, reason),
					time.Now().Add(time.Second),
				)
			}
			_ = c.conn.Close()
		}
	})
}

func (c *Client) isClosing() bool {
	return c.closing.Load()
}

func (c *Client) acknowledge(sequence uint64) {
	for {
		previous := c.lastAck.Load()
		if sequence <= previous || c.lastAck.CompareAndSwap(previous, sequence) {
			return
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.closeConnection()
		c.signalUnregister()
	}()

	for {
		var msg Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			break
		}

		if msg.Type == model.MsgTypeAck {
			if msg.RoomID != 0 && msg.RoomID != c.RoomID {
				c.hub.enqueueJSON(c, map[string]interface{}{
					"type": "error",
					"code": http.StatusBadRequest,
					"msg":  "ACK room_id 与当前房间不一致",
				})
				continue
			}
			c.acknowledge(msg.AckSeq)
			c.hub.metrics.ackTotal.Add(1)
			continue
		}

		// 匿名用户禁止发送弹幕，直接拦截并返回提示
		if c.UserID == 0 {
			c.hub.enqueueJSON(c, map[string]interface{}{
				"type": "error",
				"code": http.StatusUnauthorized,
				"msg":  "登录后才能发送弹幕",
			})
			continue
		}

		if msg.MessageID == "" {
			msg.MessageID = uuid.NewString()
		} else if _, parseErr := uuid.Parse(msg.MessageID); parseErr != nil {
			c.hub.enqueueJSON(c, map[string]interface{}{
				"type": "error",
				"code": http.StatusBadRequest,
				"msg":  "message_id 必须是 UUID",
			})
			continue
		}
		msg.UserID = c.UserID
		msg.RoomID = c.RoomID
		msg.UserName = c.UserName

		if msg.Type == "" {
			msg.Type = model.MsgTypeDanma
		}

		err = kafka.SendDanmaMsg(c.hub.ctx, msg)
		if err != nil {
			log.Printf("produce danma to kafka failed: %v", err)
			c.hub.enqueueJSON(c, map[string]interface{}{
				"type":       "error",
				"code":       http.StatusServiceUnavailable,
				"message_id": msg.MessageID,
				"msg":        "弹幕暂时发送失败，请使用相同 message_id 重试",
			})
			continue
		}
		c.hub.enqueueJSON(c, map[string]interface{}{
			"type":       model.MsgTypeAccepted,
			"message_id": msg.MessageID,
		})
	}
}

func (c *Client) writePump() {
	defer func() {
		c.closeConnection()
		c.signalUnregister()
	}()
	for {
		message, ok := <-c.send
		if !ok {
			break
		}
		if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			if !c.isClosing() {
				c.hub.metrics.writeErrorsTotal.Add(1)
			}
			break
		}
		if err := c.conn.WritePreparedMessage(message); err != nil {
			if !c.isClosing() {
				c.hub.metrics.writeErrorsTotal.Add(1)
			}
			break
		}
	}
}
