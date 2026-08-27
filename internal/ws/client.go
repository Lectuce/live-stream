package ws

import (
	"encoding/json"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/kafka"
	"live-stream/internal/model"
	"log"

	"github.com/google/uuid"

	"github.com/gorilla/websocket"
)

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	RoomID int64       // 当前客户端所在的房间
	send   chan []byte // 下发给前端的消息队列
	UserID int64       // 当前客户端的用户id
}

func NewClient(hub *Hub, conn *websocket.Conn, roomID int64, userID int64) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		RoomID: roomID,
		UserID: userID,
		send:   make(chan []byte, 256),
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
		msg.MessageID = uuid.NewString()
		msg.UserID = c.UserID
		msg.RoomID = c.RoomID
		// 广播给在线用户
		// c.hub.Broadcast <- msg
		// 发布redis
		payload, err := json.Marshal(msg)
		if err != nil {
			log.Printf("marshal websocket message failed: %v\n", err)
			continue
		}
		err = dao.Publish(c.hub.ctx, dao.DanmuChannel, string(payload))
		if err != nil {
			log.Printf("publish danmu to redis failed: %v\n", err)
		}
		// 写入kafka
		dm := model.Danma{
			MessageID: msg.MessageID,
			RoomID:    msg.RoomID,
			UserID:    msg.UserID,
			Content:   msg.Content,
		}
		var key []byte
		key = fmt.Appendf(key, "%d", msg.RoomID)
		err = kafka.SendMsg(c.hub.ctx, key, &dm)
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
