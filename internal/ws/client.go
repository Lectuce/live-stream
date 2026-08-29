package ws

import (
	"encoding/json"
	"fmt"
	"live-stream/internal/kafka"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/gorilla/websocket"
)

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	RoomID   uint64      // 当前客户端所在的房间
	send     chan []byte // 下发给前端的消息队列
	UserID   uint64      // 当前客户端的用户id
	UserName string
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

func (c *Client) readPump() {
	defer func() {
		c.conn.Close()
	}()

	for {
		var msg Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("read error: %v\n", err)
			}
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
			// 写入发送通道，不影响主循环
			select {
			case c.send <- errMsg:
			default:
			}
			continue
		}

		msg.MessageID = uuid.NewString()
		msg.UserID = c.UserID
		msg.RoomID = c.RoomID
		msg.UserName = c.UserName

		err = kafka.SendDanmaMsg(c.hub.ctx, msg)
		if err != nil {
			log.Printf("produce danma to kafka failed: %v", err)
		}

	}
}

func (c *Client) writePump() {
	defer func() {
		close(c.send)
		c.hub.unregister <- c
	}()
	for {
		msg, ok := <-c.send
		if !ok {
			break
		}
		err := c.conn.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			fmt.Printf("write error: %v\n", err)
			break
		}
	}
}
