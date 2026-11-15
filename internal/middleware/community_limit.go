package middleware

import (
	"github.com/gin-gonic/gin"
	"live-stream/internal/dao"
)

func CommunityLimit(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, ok := GetUserID(c)
		if !ok {
			c.AbortWithStatusJSON(401, gin.H{"message": "请先登录"})
			return
		}
		target := c.FullPath()
		for _, param := range c.Params {
			target += "/" + param.Value
		}
		if !CheckCommunityLimit(c, action, uid, target) {
			return
		}
		c.Next()
	}
}

// Body-based routes call this after parsing the canonical target reference.
func CheckCommunityLimit(c *gin.Context, action string, uid uint64, target string) bool {
	allowed, err := dao.AllowCommunityWrite(c.Request.Context(), action, uid, c.ClientIP(), target)
	if err != nil {
		c.AbortWithStatusJSON(503, gin.H{"message": "操作保护服务暂不可用，请稍后重试"})
		return false
	}
	if !allowed {
		c.Header("Retry-After", "60")
		c.AbortWithStatusJSON(429, gin.H{"message": "操作过于频繁，请一分钟后重试"})
		return false
	}
	return true
}
