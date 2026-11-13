package handler

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"live-stream/internal/model"
	"live-stream/internal/service"
	"net/http"
	"strconv"
)

func ListReviewVideos(c *gin.Context) {
	before, limit, ok := parseVideoPage(c)
	if !ok {
		return
	}
	page, err := service.ListReviewVideos(c.Request.Context(), c.DefaultQuery("status", "pending"), before, limit)
	if err != nil {
		videoError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page, "message": "success"})
}
func GetReviewVideo(c *gin.Context) {
	id, ok := parseVideoID(c)
	if !ok {
		return
	}
	video, err := service.GetReviewVideo(c.Request.Context(), id)
	if err != nil {
		videoError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": video, "message": "success"})
}
func PreviewReviewVideo(c *gin.Context) {
	id, ok := parseVideoID(c)
	if !ok {
		return
	}
	version, err := strconv.ParseUint(c.Query("version"), 10, 64)
	if err != nil || version == 0 {
		c.JSON(400, gin.H{"message": "缺少有效稿件版本"})
		return
	}
	video, err := service.GetReviewMedia(c.Request.Context(), id, version)
	if err != nil {
		videoError(c, err)
		return
	}
	serveStoredVideo(c, video)
}
func ReviewVideo(c *gin.Context) {
	id, uid, ok := videoMutationIdentity(c)
	if !ok {
		return
	}
	var req model.ReviewVideoRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		c.JSON(400, gin.H{"message": "审核参数错误"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		c.JSON(400, gin.H{"message": "审核请求只能包含一个对象"})
		return
	}
	if err := service.ReviewVideo(c.Request.Context(), id, uid, req); err != nil {
		videoError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "审核结果已保存"})
}
