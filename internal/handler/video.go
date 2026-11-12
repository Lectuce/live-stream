package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/service"

	"github.com/gin-gonic/gin"
)

func videoError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrVideoTitle):
		c.JSON(http.StatusBadRequest, gin.H{"message": "视频标题不能为空"})
	case errors.Is(err, service.ErrVideoNotFound):
		c.JSON(http.StatusNotFound, gin.H{"message": "视频不存在或未公开"})
	case errors.Is(err, service.ErrVideoForbidden):
		c.JSON(http.StatusForbidden, gin.H{"message": "只有视频作者可以操作"})
	case errors.Is(err, service.ErrVideoNotReady):
		c.JSON(http.StatusConflict, gin.H{"message": "视频尚未满足发布条件"})
	default:
		log.Printf("video request failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "视频操作失败"})
	}
}

func parseVideoID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("video_id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "视频ID无效"})
		return 0, false
	}
	return id, true
}

func CreateVideo(c *gin.Context) {
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
		return
	}

	var req model.CreateVideoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "视频参数无效"})
		return
	}

	video, err := service.CreateVideo(c.Request.Context(), uid, req)
	if err != nil {
		videoError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"data": video, "message": "草稿创建成功",
	})
}

func GetVideo(c *gin.Context) {
	id, ok := parseVideoID(c)
	if !ok {
		return
	}

	video, err := service.GetPublicVideo(c.Request.Context(), id)
	if err != nil {
		videoError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": video, "message": "success",
	})
}

func ListVideos(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 || limit > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "limit必须在1到50之间"})
		return
	}

	var beforeID uint64
	if value := c.Query("cursor"); value != "" {
		beforeID, err = strconv.ParseUint(value, 10, 64)
		if err != nil || beforeID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "游标无效"})
			return
		}
	}

	page, err := service.ListVideos(c.Request.Context(), beforeID, limit)
	if err != nil {
		videoError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": page, "message": "success",
	})
}

func PublishVideo(c *gin.Context) {
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
		return
	}

	id, ok := parseVideoID(c)
	if !ok {
		return
	}

	if err := service.PublishVideo(c.Request.Context(), id, uid); err != nil {
		videoError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "发布成功"})
}
