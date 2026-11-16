package handler

import (
	"errors"
	"log"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"live-stream/internal/service"
)

func watchError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repo.ErrWatchConflict):
		c.JSON(409, gin.H{"code": "watch_session_expired", "message": "播放会话或历史记录已更新，请重新加载；当前播放不会中断"})
	case errors.Is(err, repo.ErrWatchInput), errors.Is(err, service.ErrWatchCursor):
		c.JSON(400, gin.H{"message": "观看位置、序号或历史游标无效"})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(404, gin.H{"message": "视频不存在或已不可用"})
	default:
		log.Printf("watch history: %v", err)
		c.JSON(500, gin.H{"message": "观看历史暂时无法保存或读取，请稍后重试"})
	}
}

func watchActor(c *gin.Context) (uint64, bool) {
	c.Header("Cache-Control", "private, no-store")
	if c.Request.Method != "GET" {
		return commentActor(c)
	}
	actor, ok := middleware.GetUserID(c)
	if !ok || actor == 0 {
		c.JSON(401, gin.H{"message": "请先登录"})
		return 0, false
	}
	return actor, true
}

func WatchHistory(c *gin.Context) {
	actor, ok := watchActor(c)
	if !ok {
		return
	}
	if c.Request.Method == "DELETE" {
		if err := repo.DeleteWatchHistory(c.Request.Context(), actor, 0); err != nil {
			watchError(c, err)
			return
		}
		c.JSON(200, gin.H{"message": "观看历史已清空"})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		watchError(c, service.ErrWatchCursor)
		return
	}
	page, err := service.GetWatchHistoryPage(c.Request.Context(), actor, c.Query("cursor"), limit)
	if err != nil {
		watchError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page})
}

func WatchVideoHistory(c *gin.Context) {
	actor, ok := watchActor(c)
	if !ok {
		return
	}
	video, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	if c.Request.Method == "DELETE" {
		if err := repo.DeleteWatchHistory(c.Request.Context(), actor, video); err != nil {
			watchError(c, err)
			return
		}
		c.JSON(200, gin.H{"message": "历史记录已删除"})
		return
	}
	data, err := repo.GetWatchProgress(c.Request.Context(), actor, video)
	if err != nil {
		watchError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": data})
}

func validWatchSession(req *model.WatchSessionRequest) bool {
	id, err := uuid.Parse(req.SessionID)
	if err != nil || id == uuid.Nil || req.HistoryEpoch == 0 || req.SessionVersion == ^uint64(0) {
		return false
	}
	req.SessionID = id.String()
	return true
}

func WatchSession(c *gin.Context) {
	actor, ok := watchActor(c)
	if !ok {
		return
	}
	video, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	var req model.WatchSessionRequest
	if !commentBody(c, &req) {
		return
	}
	if !validWatchSession(&req) {
		watchError(c, repo.ErrWatchInput)
		return
	}
	data, err := repo.StartWatchSession(c.Request.Context(), actor, video, req)
	if err != nil {
		watchError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": data})
}

func WatchProgress(c *gin.Context) {
	actor, ok := watchActor(c)
	if !ok {
		return
	}
	video, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	var req model.WatchProgressRequest
	if !commentBody(c, &req) {
		return
	}
	if !validWatchSession(&req.WatchSessionRequest) || req.PositionMS == nil || req.Seq == 0 || req.Seq > 9007199254740991 {
		watchError(c, repo.ErrWatchInput)
		return
	}
	if err := repo.SaveWatchProgress(c.Request.Context(), actor, video, req); err != nil {
		watchError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "进度已保存"})
}
