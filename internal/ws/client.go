package ws

import (
	"encoding/json"
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
	RoomID   uint64      // 当前客户端所在的房间
	send     chan []byte // 下发给前端的消息队列
	UserID   uint64      // 当前客户端的用户id
	UserName string

	sendMu         sync.RWMutex
	sendClosed     bool
	unregisterOnce sync.Once
	closeOnce      sync.Once
	closing        atomic.Bool
}

func NewClient(hub *Hub, conn *websocket.Conn, roomID uint64, userID uint64, userName string) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		RoomID:   roomID,
		UserID:   userID,
		UserName: userName,
		send:     make(chan []byte, 256),
	}
}

func (c *Client) trySend(data []byte) enqueueResult {
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.sendClosed {
		return enqueueClosed
	}
	select {
	case c.send <- data:
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
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

func (c *Client) isClosing() bool {
	return c.closing.Load()
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

		// 匿名用户禁止发送弹幕，直接拦截并返回提示
		if c.UserID == 0 {
			// 构造错误消息回写给当前客户端
			errMsg, _ := json.Marshal(map[string]interface{}{
				"type": "error",
				"code": http.StatusUnauthorized,
				"msg":  "登录后才能发送弹幕",
			})
			c.hub.enqueueClient(c, errMsg)
			continue
		}

		msg.MessageID = uuid.NewString()
		msg.UserID = c.UserID
		msg.RoomID = c.RoomID
		msg.UserName = c.UserName

		if msg.Type == "" {
			msg.Type = model.MsgTypeDanma
		}

		err = kafka.SendDanmaMsg(c.hub.ctx, msg)
		if err != nil {
			log.Printf("produce danma to kafka failed: %v", err)
		}

	}
}

func (c *Client) writePump() {
	defer func() {
		c.closeConnection()
		c.signalUnregister()
	}()
	for {
		msg, ok := <-c.send
		if !ok {
			break
		}
		if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			if !c.isClosing() {
				c.hub.metrics.writeErrorsTotal.Add(1)
			}
			break
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			if !c.isClosing() {
				c.hub.metrics.writeErrorsTotal.Add(1)
			}
			break
		}
	}
}
