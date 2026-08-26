package ws

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,

	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func ServeWs(hub *Hub, c *gin.Context) {
	roomIDStr := c.Query("room_id")
	userIDStr := c.Query("user_id")
	rid, err := strconv.ParseInt(roomIDStr, 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{
			"msg": "room_id 格式错误",
		})
		return
	}
	uid, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{
			"msg": "user_id 格式错误",
		})
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{
			"msg": "websocket upgrade failed",
		})
		return
	}
	client := NewClient(hub, conn, rid, uid)
	client.hub.register <- client
	go client.writePump()
	go client.readPump()
}
