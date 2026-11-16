package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"strconv"
	"strings"
)

type danmakuCursor struct {
	Video, From, To, Position, ID uint64
	Version                       int
}

func parseDanmakuWindow(c *gin.Context, video uint64) (danmakuCursor, int, bool) {
	from, e1 := strconv.ParseUint(c.Query("from_ms"), 10, 64)
	to, e2 := strconv.ParseUint(c.Query("to_ms"), 10, 64)
	limit, e3 := strconv.Atoi(c.DefaultQuery("limit", "100"))
	cursor := danmakuCursor{Video: video, From: from, To: to, Version: 1}
	if e1 != nil || e2 != nil || e3 != nil || to <= from || to-from > 30000 || limit < 1 || limit > 100 {
		return cursor, 0, false
	}
	if raw := c.Query("cursor"); raw != "" {
		if len(raw) > 512 {
			return cursor, 0, false
		}
		bytes, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return cursor, 0, false
		}
		var next danmakuCursor
		if json.Unmarshal(bytes, &next) != nil || next.Version != 1 || next.Video != video || next.From != from || next.To != to || next.ID == 0 || next.Position < from || next.Position >= to {
			return cursor, 0, false
		}
		cursor = next
	}
	return cursor, limit, true
}
func VideoDanmakus(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	video, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	if c.Request.Method == "GET" {
		cursor, limit, ok := parseDanmakuWindow(c, video)
		if !ok {
			c.JSON(400, gin.H{"message": "弹幕时间窗口或游标无效，窗口最多30秒"})
			return
		}
		items, err := repo.ListVideoDanmakus(c.Request.Context(), video, cursor.From, cursor.To, cursor.Position, cursor.ID, limit+1)
		if err != nil {
			interactionError(c, err)
			return
		}
		more := len(items) > limit
		next := ""
		if more {
			items = items[:limit]
			last := items[len(items)-1]
			cursor.Position = last.PositionMS
			cursor.ID = last.ID
			raw, _ := json.Marshal(cursor)
			next = base64.RawURLEncoding.EncodeToString(raw)
		}
		visible := make([]model.VideoDanmaku, 0, len(items))
		for _, item := range items {
			if !item.Hidden && item.DeletedAt == nil {
				visible = append(visible, item)
			}
		}
		c.JSON(200, gin.H{"data": gin.H{"items": visible, "has_more": more, "next_cursor": next}})
		return
	}
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	var req struct {
		PositionMS *uint64 `json:"position_ms"`
		Content    string  `json:"content"`
		Color      string  `json:"color"`
		RequestID  string  `json:"request_id"`
	}
	if !commentBody(c, &req) {
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	key, err := uuid.Parse(req.RequestID)
	if req.Color == "" {
		req.Color = "#ffffff"
	}
	if req.PositionMS == nil || !validText(req.Content, 100) || strings.ContainsAny(req.Content, "\r\n") || err != nil || key == uuid.Nil || (req.Color != "#ffffff" && req.Color != "#ffe066" && req.Color != "#ff8a80") {
		c.JSON(400, gin.H{"message": "正文须为1–100字单行文字，颜色或请求ID无效"})
		return
	}
	item := model.VideoDanmaku{VideoID: video, UserID: actor, PositionMS: *req.PositionMS, Content: req.Content, Color: req.Color, RequestID: key.String()}
	if err := repo.CreateVideoDanmaku(c.Request.Context(), &item); err != nil {
		if errors.Is(err, repo.ErrDanmakuPosition) {
			c.JSON(400, gin.H{"message": "发送位置超出视频时长，或视频尚无有效时长"})
			return
		}
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": item})
}
func DeleteVideoDanmaku(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	video, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	id, ok := positiveID(c, "danmaku_id")
	if !ok {
		return
	}
	if err := repo.DeleteVideoDanmaku(c.Request.Context(), video, id, actor); err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "弹幕已删除"})
}
