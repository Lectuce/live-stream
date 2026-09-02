package middleware

import (
	"live-stream/internal/config"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS permits credentialed browser clients only from the same allowlist used
// by the WebSocket Origin check. Unknown origins receive no CORS permission.
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
