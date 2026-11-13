package handler

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/service"
	"net/http"
)

func videoMutationIdentity(c *gin.Context) (uint64, uint64, bool) {
	c.Header("Cache-Control", "private, no-store")
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(401, gin.H{"message": "未登录"})
		return 0, 0, false
	}
	if !trustedUploadOrigin(c) {
		c.JSON(403, gin.H{"message": "操作来源不允许"})
		return 0, 0, false
	}
	id, ok := parseVideoID(c)
	return id, uid, ok
}

func EditVideo(c *gin.Context) {
	id, uid, ok := videoMutationIdentity(c)
	if !ok {
		return
	}
	var req model.UpdateVideoRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		c.JSON(400, gin.H{"message": "只允许修改标题和简介，请提交有效 JSON"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		c.JSON(400, gin.H{"message": "请求体只能包含一个 JSON 对象"})
		return
	}
	video, err := service.EditVideo(c.Request.Context(), id, uid, req)
	if err != nil {
		videoError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": video, "message": "稿件已保存"})
}

func UnpublishVideo(c *gin.Context) {
	id, uid, ok := videoMutationIdentity(c)
	if !ok {
		return
	}
	video, err := service.UnpublishVideo(c.Request.Context(), id, uid)
	if err != nil {
		videoError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": video, "message": "已下架，稿件已退回草稿"})
}

func DeleteVideo(c *gin.Context) {
	id, uid, ok := videoMutationIdentity(c)
	if !ok {
		return
	}
	if err := service.DeleteVideo(c.Request.Context(), id, uid); err != nil {
		videoError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "稿件已删除"})
}
