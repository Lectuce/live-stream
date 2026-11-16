package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/service"
	"strconv"
)

func PrepareLive(c *gin.Context) {
	owner, ok := commentActor(c)
	if !ok {
		return
	}
	room, err := strconv.ParseUint(c.Param("room_id"), 10, 64)
	if err != nil || room == 0 {
		c.JSON(400, gin.H{"message": "房间ID无效"})
		return
	}
	if _, err := config.LiveMediaConfig(); err != nil {
		c.JSON(503, gin.H{"message": "直播媒体尚未配置，请配置 LIVE_MEDIA_SECRET、LIVE_PUSH_BASE 和 LIVE_PLAY_BASE"})
		return
	}
	var req model.StartRoomRequest
	if !commentBody(c, &req) {
		return
	}
	if len([]rune(req.RoomTitle)) < 1 || len([]rune(req.RoomTitle)) > 100 {
		c.JSON(400, gin.H{"message": "标题需要1–100字"})
		return
	}
	result, err := service.PrepareLive(c.Request.Context(), room, owner, req.RoomTitle)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"message": "房间不存在或无权开播"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"message": "申请推流失败"})
		return
	}
	c.JSON(200, gin.H{"data": result, "message": "请使用本场推流凭证连接 OBS"})
}

func StopMediaLive(c *gin.Context) {
	owner, ok := commentActor(c)
	if !ok {
		return
	}
	room, err := strconv.ParseUint(c.Param("room_id"), 10, 64)
	if err != nil || room == 0 {
		c.JSON(400, gin.H{"message": "房间ID无效"})
		return
	}
	if err := service.StopMediaLive(c.Request.Context(), room, owner); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"message": "房间不存在或无权关播"})
		} else {
			c.JSON(500, gin.H{"message": "关播失败"})
		}
		return
	}
	c.JSON(200, gin.H{"message": "关播成功"})
}

func FollowingLive(c *gin.Context) {
	viewer, ok := commentActor(c)
	if !ok {
		return
	}
	before := uint64(0)
	if value := c.Query("cursor"); value != "" {
		var err error
		before, err = strconv.ParseUint(value, 10, 64)
		if err != nil || before == 0 {
			c.JSON(400, gin.H{"message": "游标无效"})
			return
		}
	}
	items := make([]model.HotRoomDTO, 0)
	q := dao.SC.Db.WithContext(c.Request.Context()).Table("rooms AS r").Select("r.room_id,r.room_title,r.owner_id,r.like_count,r.play_url,u.user_name AS owner_username").Joins("JOIN user_follows AS f ON f.followee_id = r.owner_id AND f.follower_id = ?", viewer).Joins("JOIN users AS u ON u.user_id = r.owner_id").Where("r.status = 1")
	if before > 0 {
		q = q.Where("r.room_id < ?", before)
	}
	if err := q.Order("r.room_id DESC").Limit(21).Scan(&items).Error; err != nil {
		c.JSON(500, gin.H{"message": "读取关注直播失败"})
		return
	}
	more := len(items) > 20
	cursor := ""
	if more {
		items = items[:20]
		cursor = strconv.FormatUint(items[19].RoomID, 10)
	}
	c.JSON(200, gin.H{"data": model.LiveFeedPage{Items: items, HasMore: more, NextCursor: cursor}})
}
