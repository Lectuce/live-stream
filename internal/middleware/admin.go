package middleware

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"live-stream/internal/model"
	"live-stream/internal/repo"
)

func RequireAdmin(c *gin.Context) { requireAdmin(c, repo.SelectUserByUserID) }

func requireAdmin(c *gin.Context, lookup func(context.Context, uint64) (*model.User, error)) {
	c.Header("Cache-Control", "private, no-store")
	uid, ok := GetUserID(c)
	if !ok {
		c.AbortWithStatusJSON(401, gin.H{"message": "未登录"})
		return
	}
	user, err := lookup(c.Request.Context(), uid)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && user == nil) {
		c.AbortWithStatusJSON(401, gin.H{"message": "账号不存在"})
		return
	}
	if err != nil {
		c.AbortWithStatusJSON(500, gin.H{"message": "权限检查失败"})
		return
	}
	if user.Role != "admin" {
		c.AbortWithStatusJSON(403, gin.H{"message": "需要管理员权限"})
		return
	}
	c.Next()
}
