package ws

import (
	"live-stream/internal/auth"
	"live-stream/internal/config"
	"live-stream/internal/repo"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func checkOrigin(r *http.Request) bool {
	if config.GlobalConfig == nil {
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	for _, allowed := range config.GlobalConfig.Server.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}

	return false
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	Subprotocols:    []string{"vite-live-stream-v1"},

	CheckOrigin: checkOrigin,
}

func ServeWs(hub *Hub, c *gin.Context) {
	roomIDStr := c.Query("room_id")
	rid, err := strconv.ParseUint(roomIDStr, 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{
			"msg": "room_id 格式错误",
		})
		return
	}
	var uid uint64
	var uname string
	sid, err := c.Cookie("sid")
	if err == nil {
		parseUID, parseErr := auth.GetSessionByToken(c.Request.Context(), sid)
		if parseErr == nil && parseUID > 0 {
			uid = parseUID
			user, err := repo.SelectUserByUserID(c.Request.Context(), uid)
			if err == nil {
				uname = user.UserName
			}
		}

	}

	room, err := repo.SelectRoomByRoomID(c, rid)
	if err != nil || room == nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
			"message": "房间不存在",
		})
		return
	}

	if room.Status != 1 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"message": "房间尚未开播",
		})
		return
	}

	// 升级websocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{
			"msg": "websocket upgrade failed",
		})
		return
	}

	client := NewClient(hub, conn, rid, uid, uname, room.LikeCount)
	client.hub.register <- client
	go client.writePump()

	// 对端离开前，升级后的连接始终由此处理函数持有。
	// 只有写入端需要单独的协程；readPump 是唯一的读取端，
	// 返回时负责清理连接并注销。
	client.readPump()
}
