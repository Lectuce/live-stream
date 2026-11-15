package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"live-stream/internal/auth"
	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"live-stream/internal/service"
)

func optionalCommentViewer(c *gin.Context) uint64 {
	if uid, ok := middleware.GetUserID(c); ok {
		return uid
	}
	token, err := c.Cookie("sid")
	if err != nil {
		return 0
	}
	uid, err := auth.GetSessionByToken(c.Request.Context(), token)
	if err != nil {
		return 0
	}
	return uid
}

func commentBody(c *gin.Context, body any) bool {
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	d.DisallowUnknownFields()
	if d.Decode(body) != nil || d.Decode(new(any)) != io.EOF {
		c.JSON(400, gin.H{"message": "参数无效，请检查正文和版本"})
		return false
	}
	return true
}

func commentMutation(c *gin.Context) (uint64, uint64, bool) {
	actor, ok := commentActor(c)
	if !ok {
		return 0, 0, false
	}
	id, ok := parseCommentID(c)
	return id, actor, ok
}

func EditComment(c *gin.Context) {
	id, actor, ok := commentMutation(c)
	if !ok {
		return
	}
	var req model.EditCommentRequest
	if !commentBody(c, &req) {
		return
	}
	if err := service.EditComment(c.Request.Context(), id, actor, req); err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "修改已提交，等待审核"})
}

func ReviewComment(c *gin.Context) {
	id, actor, ok := commentMutation(c)
	if !ok {
		return
	}
	var req model.ReviewCommentRequest
	if !commentBody(c, &req) {
		return
	}
	if err := service.ReviewComment(c.Request.Context(), id, actor, req); err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "审核结果已保存"})
}

func LikeComment(c *gin.Context) {
	id, actor, ok := commentMutation(c)
	if !ok {
		return
	}
	liked := c.Request.Method == "PUT"
	count, err := repo.SetCommentLike(c.Request.Context(), id, actor, liked)
	if err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"liked": liked, "like_count": count}, "message": "success"})
}

func ReportComment(c *gin.Context) {
	id, actor, ok := commentMutation(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !commentBody(c, &req) {
		return
	}
	if err := service.ReportComment(c.Request.Context(), id, actor, req.Reason); err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "举报已提交，请勿重复提交"})
}

func commentAdminPage(c *gin.Context) (uint64, int, bool) {
	c.Header("Cache-Control", "private, no-store")
	before, err := strconv.ParseUint(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, e := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || e != nil || limit < 1 || limit > 50 {
		c.JSON(400, gin.H{"message": "分页参数无效"})
		return 0, 0, false
	}
	return before, limit, true
}

func commentPageResult[T any](c *gin.Context, items []T, limit int, id func(T) uint64) {
	more := len(items) > limit
	cursor := ""
	if more {
		items = items[:limit]
		cursor = strconv.FormatUint(id(items[len(items)-1]), 10)
	}
	c.JSON(200, gin.H{"data": gin.H{"items": items, "has_more": more, "next_cursor": cursor}, "message": "success"})
}

func ListMyComments(c *gin.Context) {
	before, limit, ok := commentAdminPage(c)
	if !ok {
		return
	}
	actor, _ := middleware.GetUserID(c)
	kind := c.Query("target_type")
	target, err := strconv.ParseUint(c.Query("target_id"), 10, 64)
	if err != nil || target == 0 || (kind != "video" && kind != "post") {
		c.JSON(400, gin.H{"message": "内容参数无效"})
		return
	}
	items, err := repo.ListOwnComments(c.Request.Context(), actor, kind, target, before, limit+1)
	if err != nil {
		commentError(c, err)
		return
	}
	if err := repo.FillCommentLikes(c.Request.Context(), items, actor); err != nil {
		commentError(c, err)
		return
	}
	commentPageResult(c, items, limit, func(v model.CommentView) uint64 { return v.ID })
}

func ListReviewComments(c *gin.Context) {
	before, limit, ok := commentAdminPage(c)
	if !ok {
		return
	}
	status := c.DefaultQuery("status", "pending")
	if status != "pending" && status != "approved" && status != "rejected" && status != "hidden" {
		c.JSON(400, gin.H{"message": "审核状态无效"})
		return
	}
	items, err := repo.ListReviewComments(c.Request.Context(), status, before, limit+1)
	if err != nil {
		commentError(c, err)
		return
	}
	commentPageResult(c, items, limit, func(v model.CommentView) uint64 { return v.ID })
}

func CommentHistory(c *gin.Context) {
	before, limit, ok := commentAdminPage(c)
	if !ok {
		return
	}
	id, ok := parseCommentID(c)
	if !ok {
		return
	}
	items, err := repo.CommentHistory(c.Request.Context(), id, before, limit+1)
	if err != nil {
		commentError(c, err)
		return
	}
	commentPageResult(c, items, limit, func(v model.CommentReview) uint64 { return v.ID })
}

func ListCommentReports(c *gin.Context) {
	before, limit, ok := commentAdminPage(c)
	if !ok {
		return
	}
	status := c.DefaultQuery("status", "pending")
	if status != "pending" && status != "resolved" && status != "dismissed" {
		c.JSON(400, gin.H{"message": "举报状态无效"})
		return
	}
	items, err := repo.ListCommentReports(c.Request.Context(), status, before, limit+1)
	if err != nil {
		commentError(c, err)
		return
	}
	commentPageResult(c, items, limit, func(v model.CommentReportView) uint64 { return v.ID })
}

func ResolveCommentReport(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("report_id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"message": "举报ID无效"})
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !commentBody(c, &req) {
		return
	}
	if err := service.ResolveCommentReport(c.Request.Context(), id, actor, req.Status, req.Reason); err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "处理结果已保存"})
}

func RebuildCommentLikes(c *gin.Context) {
	if _, ok := commentActor(c); !ok {
		return
	}
	var req struct {
		After uint64 `json:"after,string"`
	}
	if !commentBody(c, &req) {
		return
	}
	next, err := repo.RebuildCommentLikes(c.Request.Context(), req.After, 200)
	if err != nil {
		commentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"next_after": strconv.FormatUint(next, 10), "done": next == req.After}, "message": "success"})
}
