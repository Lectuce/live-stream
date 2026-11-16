package handler

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"live-stream/internal/service"
)

func discoveryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidFeedCursor):
		c.JSON(400, gin.H{"message": "排序、分区、搜索词或游标无效"})
	case errors.Is(err, dao.ErrVideoSnapshotExpired):
		c.JSON(410, gin.H{"message": "列表已过期，请刷新重新加载"})
	case errors.Is(err, dao.ErrVideoDiscoveryUnavailable):
		c.JSON(503, gin.H{"message": "推荐服务暂不可用，请稍后重试或切换最新"})
	default:
		log.Printf("video discovery: %v", err)
		c.JSON(503, gin.H{"message": "内容暂时无法加载，请稍后重试"})
	}
}

// Read requests are IP-limited when Redis is healthy; bounded SQL fallback is
// still available during cache failures. No global guest-user bucket.
func discoveryReadAllowed(c *gin.Context) bool {
	c.Header("Cache-Control", "private, no-store")
	if dao.SC == nil || dao.SC.Redis == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 150*time.Millisecond)
	defer cancel()
	key := fmt.Sprintf("discovery:read:%x", sha256.Sum256([]byte(c.ClientIP())))
	n, e := dao.SC.Redis.Eval(ctx, `local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],60) end;return n`, []string{key}).Int()
	if e == nil && n > 120 {
		c.Header("Retry-After", "60")
		c.JSON(429, gin.H{"message": "浏览请求过于频繁，请稍后重试"})
		return false
	}
	return true
}
func VideoDiscovery(c *gin.Context) {
	if !discoveryReadAllowed(c) {
		return
	}
	limit, e := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if e != nil {
		discoveryError(c, service.ErrInvalidFeedCursor)
		return
	}
	p, e := service.GetVideoDiscovery(c.Request.Context(), c.DefaultQuery("sort", "recommended"), c.Query("category"), c.Query("cursor"), limit)
	if e != nil {
		discoveryError(c, e)
		return
	}
	c.JSON(200, gin.H{"data": p})
}
func VideoCategories(c *gin.Context) { c.JSON(200, gin.H{"data": model.VideoCategories}) }
func SearchDiscovery(c *gin.Context) {
	if !discoveryReadAllowed(c) {
		return
	}
	limit, e := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if e != nil {
		discoveryError(c, service.ErrInvalidFeedCursor)
		return
	}
	p, e := service.SearchDiscovery(c.Request.Context(), c.DefaultQuery("type", "video"), strings.TrimSpace(c.Query("q")), c.Query("cursor"), limit)
	if e != nil {
		discoveryError(c, e)
		return
	}
	c.JSON(200, gin.H{"data": p})
}
func VideoBehavior(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	id, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	var req struct {
		Kind          string `json:"kind"`
		DeliveryToken string `json:"delivery_token"`
	}
	if !commentBody(c, &req) {
		return
	}
	if req.Kind != "impression" && req.Kind != "click" {
		watchError(c, repo.ErrWatchInput)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if !dao.ValidVideoDelivery(ctx, req.DeliveryToken, id) {
		c.JSON(400, gin.H{"message": "内容来源已过期，请刷新列表"})
		return
	}
	if e := repo.RecordVideoBehavior(ctx, actor, id, req.Kind); e != nil {
		watchError(c, e)
		return
	}
	c.JSON(200, gin.H{"message": "已记录"})
}
func VideoPlaySession(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	video, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	if c.Request.Method == "POST" {
		s, e := repo.StartVideoPlay(c.Request.Context(), actor, video)
		if e != nil {
			watchError(c, e)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"session_id": s.ID, "expires_at": s.ExpiresAt}})
		return
	}
	var req struct {
		SessionID  string  `json:"session_id"`
		Seq        uint64  `json:"seq"`
		PositionMS *uint64 `json:"position_ms"`
	}
	if !commentBody(c, &req) {
		return
	}
	id, e := uuid.Parse(req.SessionID)
	if e != nil || id == uuid.Nil || req.PositionMS == nil {
		watchError(c, repo.ErrWatchInput)
		return
	}
	qualified, e := repo.PulseVideoPlay(c.Request.Context(), actor, video, id.String(), req.Seq, *req.PositionMS)
	if e != nil {
		watchError(c, e)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"qualified": qualified}})
}
