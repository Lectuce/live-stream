package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"live-stream/internal/middleware"
	"live-stream/internal/service"
	"strconv"
)

func GetFollowingFeed(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	uid, ok := middleware.GetUserID(c)
	if !ok || uid == 0 {
		c.JSON(401, gin.H{"message": "未登录"})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 || limit > 50 {
		c.JSON(400, gin.H{"message": "limit必须在1到50之间"})
		return
	}
	page, err := service.GetFollowingFeed(c.Request.Context(), uid, c.Query("cursor"), limit)
	if errors.Is(err, service.ErrFollowingCursor) {
		c.JSON(400, gin.H{"message": "动态游标无效，请刷新列表"})
		return
	}
	if err != nil {
		postError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page, "message": "success"})
}
