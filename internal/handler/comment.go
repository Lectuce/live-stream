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
	"live-stream/internal/dao"
	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"live-stream/internal/service"
)

func commentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(404, gin.H{"message": "内容或评论不存在、不可见，或无权操作"})
	case errors.Is(err, repo.ErrCommentConflict):
		c.JSON(409, gin.H{"message": "评论已发生变化，请刷新后操作"})
	case errors.Is(err, dao.ErrCommentSnapshotExpired):
		c.JSON(410, gin.H{"message": "热门评论已过期，请刷新评论"})
	case errors.Is(err, service.ErrCommentInput):
		c.JSON(400, gin.H{"message": "参数无效：正文1–2000字，理由至多1000字，请检查请求ID和版本"})
	case errors.Is(err, service.ErrCommentCursor):
		c.JSON(400, gin.H{"message": "评论游标无效，请刷新"})
	case errors.Is(err, repo.ErrCommentTarget):
		c.JSON(400, gin.H{"message": "回复目标不属于当前内容或不存在"})
	case errors.Is(err, repo.ErrCommentClosed):
		c.JSON(409, gin.H{"message": "评论已删除或未公开，该讨论暂不接受互动"})
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

func ListVideoComments(c *gin.Context)       { listCommentPage(c, model.CommentTargetVideo, false) }
func ListVideoCommentReplies(c *gin.Context) { listCommentPage(c, model.CommentTargetVideo, true) }
func listCommentPage(c *gin.Context, targetType string, replies bool) {
	c.Header("Cache-Control", "private, no-store")
	targetID, ok := parseCommentTargetID(c, targetType)
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
	page, err := service.GetCommentPage(c.Request.Context(), targetType, targetID, root, optionalCommentViewer(c), c.DefaultQuery("sort", "latest"), c.Query("cursor"), limit)
	if err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page, "message": "success"})
}

func createComment(c *gin.Context, targetType string) {
	uid, ok := commentActor(c)
	if !ok {
		return
	}
	targetID, ok := parseCommentTargetID(c, targetType)
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
	comment, err := service.CreateComment(c.Request.Context(), targetType, targetID, uid, req)
	if err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": comment, "message": "评论已提交，等待审核"})
}

func deleteComment(c *gin.Context, targetType string) {
	uid, ok := commentActor(c)
	if !ok {
		return
	}
	targetID, ok := parseCommentTargetID(c, targetType)
	if !ok {
		return
	}
	id, ok := parseCommentID(c)
	if !ok {
		return
	}
	if err := repo.DeleteComment(c.Request.Context(), targetType, targetID, id, uid); err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "评论已删除"})
}

func parseCommentTargetID(c *gin.Context, targetType string) (uint64, bool) {
	param := "video_id"
	if targetType == model.CommentTargetPost {
		param = "post_id"
	}
	id, err := strconv.ParseUint(c.Param(param), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"message": "内容ID无效"})
		return 0, false
	}
	return id, true
}
func CreateVideoComment(c *gin.Context)     { createComment(c, model.CommentTargetVideo) }
func DeleteVideoComment(c *gin.Context)     { deleteComment(c, model.CommentTargetVideo) }
func ListPostComments(c *gin.Context)       { listCommentPage(c, model.CommentTargetPost, false) }
func ListPostCommentReplies(c *gin.Context) { listCommentPage(c, model.CommentTargetPost, true) }
func CreatePostComment(c *gin.Context)      { createComment(c, model.CommentTargetPost) }
func DeletePostComment(c *gin.Context)      { deleteComment(c, model.CommentTargetPost) }
