package middleware

import (
	"live-stream/internal/config"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS 仅允许来自 WebSocket Origin 检查所用同一白名单的浏览器客户端携带凭据。
// 未知来源不会获得任何 CORS 权限。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" || config.GlobalConfig == nil || !allowedOrigin(origin) {
			if c.Request.Method == http.MethodOptions && origin != "" {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PATCH,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func allowedOrigin(origin string) bool {
	for _, candidate := range config.GlobalConfig.Server.AllowedOrigins {
		if origin == candidate {
			return true
		}
	}
	return false
}
