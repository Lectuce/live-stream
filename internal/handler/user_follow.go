package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"live-stream/internal/middleware"
	"live-stream/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func parsePublicUserID(c *gin.Context) (uint64, bool) {
	c.Header("Cache-Control", "private, no-store")
	id, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"message": "用户ID无效"})
		return 0, false
	}
	return id, true
}

func userFollowError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrSelfFollow):
		c.JSON(400, gin.H{"message": "不能关注自己"})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(404, gin.H{"message": "用户不存在"})
	default:
		log.Printf("user follow request failed: %v", err)
		c.JSON(500, gin.H{"message": "操作失败，请稍后重试"})
	}
}

func GetPublicUser(c *gin.Context) {
	id, ok := parsePublicUserID(c)
	if !ok {
		return
	}
	user, err := service.GetPublicUser(c.Request.Context(), id)
	if err != nil {
		userFollowError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": user, "message": "success"})
}

func ListPublicAuthorVideos(c *gin.Context) {
	id, ok := parsePublicUserID(c)
	if !ok {
		return
	}
	cursor, limit, ok := parseVideoPage(c)
	if !ok {
		return
	}
	page, err := service.ListPublicAuthorVideos(c.Request.Context(), id, cursor, limit)
	if err != nil {
		userFollowError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page, "message": "success"})
}

// The actor always comes from the authenticated session, never from JSON/query parameters.
func UserFollow(c *gin.Context) {
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(401, gin.H{"message": "未登录"})
		return
	}
	id, ok := parsePublicUserID(c)
	if !ok {
		return
	}
	if c.Request.Method == http.MethodGet {
		state, err := service.GetFollowState(c.Request.Context(), uid, id)
		if err != nil {
			userFollowError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": state, "message": "success"})
		return
	}
	if !trustedUploadOrigin(c) {
		c.JSON(403, gin.H{"message": "操作来源不允许"})
		return
	}
	state, err := service.SetFollow(c.Request.Context(), uid, id, c.Request.Method == http.MethodPut)
	if err != nil {
		userFollowError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": state, "message": "success"})
}
