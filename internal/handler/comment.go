package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"live-stream/internal/service"
)

func commentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(404, gin.H{"message": "视频或评论不存在、不可见，或无权操作"})
	case errors.Is(err, service.ErrCommentInput):
		c.JSON(400, gin.H{"message": "评论须为1–2000字，并提供有效的request_id"})
	case errors.Is(err, service.ErrCommentCursor):
		c.JSON(400, gin.H{"message": "评论游标无效，请刷新"})
	case errors.Is(err, repo.ErrCommentTarget):
		c.JSON(400, gin.H{"message": "回复目标不属于当前视频或不存在"})
	case errors.Is(err, repo.ErrCommentClosed):
		c.JSON(409, gin.H{"message": "评论已删除，该讨论不再接受此回复"})
	case errors.Is(err, repo.ErrCommentRequest):
		c.JSON(409, gin.H{"message": "request_id已用于另一条评论"})
	default:
		log.Printf("comment request: %v", err)
		c.JSON(500, gin.H{"message": "评论操作失败，请稍后重试"})
	}
}

func commentActor(c *gin.Context) (uint64, bool) {
	c.Header("Cache-Control", "private, no-store")
	uid, ok := middleware.GetUserID(c)
	if !ok || uid == 0 {
		c.JSON(401, gin.H{"message": "请先登录"})
		return 0, false
	}
	if !trustedUploadOrigin(c) {
		c.JSON(403, gin.H{"message": "操作来源不允许"})
		return 0, false
	}
	return uid, true
}

func parseCommentID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("comment_id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"message": "评论ID无效"})
		return 0, false
	}
	return id, true
}

func ListVideoComments(c *gin.Context)       { listCommentPage(c, false) }
func ListVideoCommentReplies(c *gin.Context) { listCommentPage(c, true) }
func listCommentPage(c *gin.Context, replies bool) {
	c.Header("Cache-Control", "private, no-store")
	videoID, ok := parseVideoID(c)
	if !ok {
		return
	}
	var root uint64
	if replies {
		root, ok = parseCommentID(c)
		if !ok {
			return
		}
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 || limit > 50 {
		c.JSON(400, gin.H{"message": "limit必须在1到50之间"})
		return
	}
	page, err := service.ListVideoComments(c.Request.Context(), videoID, root, c.Query("cursor"), limit)
	if err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page, "message": "success"})
}

func CreateVideoComment(c *gin.Context) {
	uid, ok := commentActor(c)
	if !ok {
		return
	}
	videoID, ok := parseVideoID(c)
	if !ok {
		return
	}
	var req model.CreateCommentRequest
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		c.JSON(400, gin.H{"message": "评论参数无效"})
		return
	}
	if d.Decode(new(any)) != io.EOF {
		c.JSON(400, gin.H{"message": "请求只能包含一个JSON对象"})
		return
	}
	comment, err := service.CreateVideoComment(c.Request.Context(), videoID, uid, req)
	if err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": comment, "message": "评论已发送"})
}

func DeleteVideoComment(c *gin.Context) {
	uid, ok := commentActor(c)
	if !ok {
		return
	}
	videoID, ok := parseVideoID(c)
	if !ok {
		return
	}
	id, ok := parseCommentID(c)
	if !ok {
		return
	}
	if err := repo.DeleteVideoComment(c.Request.Context(), videoID, id, uid); err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "评论已删除"})
}
