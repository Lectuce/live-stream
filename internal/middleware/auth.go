package middleware

import (
	"live-stream/internal/auth"
	"net/http"

	"github.com/gin-gonic/gin"
)

func Auth(c *gin.Context) {
	token, err := c.Cookie("sid")
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
		return
	}
	uid, err := auth.GetSessionByToken(c.Request.Context(), token)
	if err != nil || uid == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "会话失效"})
		return
	}
	c.Set("user_id", uid)
	c.Next()
}

func GetUserID(c *gin.Context) (uint64, bool) {
	val, ok := c.Get("user_id")
	if !ok {
		return 0, false
	}
	uid, ok := val.(uint64)
	return uid, ok
}
