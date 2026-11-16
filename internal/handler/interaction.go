package handler

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/repo"
)

func interactionError(c *gin.Context, err error) {
	if errors.Is(err, repo.ErrFolderDefault) {
		c.JSON(409, gin.H{"message": "默认收藏夹不能删除"})
		return
	}
	if errors.Is(err, repo.ErrFolderLimit) {
		c.JSON(409, gin.H{"message": "最多创建100个收藏夹"})
		return
	}
	if errors.Is(err, repo.ErrCommentConflict) {
		c.JSON(409, gin.H{"message": "内容或处理状态已发生变化，请刷新后操作"})
		return
	}
	commentError(c, err)
}
func contentLike(c *gin.Context, kind string) {
	c.Header("Cache-Control", "private, no-store")
	id, ok := parseCommentTargetID(c, kind)
	if !ok {
		return
	}
	var state *model.ContentLikeState
	var err error
	if c.Request.Method == "GET" {
		state, err = repo.ContentLikeState(c.Request.Context(), kind, id, optionalCommentViewer(c))
	} else {
		actor, ok := commentActor(c)
		if !ok {
			return
		}
		state, err = repo.SetContentLike(c.Request.Context(), kind, id, actor, c.Request.Method == "PUT")
	}
	if err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": state, "message": "success"})
}
func VideoLike(c *gin.Context) { contentLike(c, "video") }
func PostLike(c *gin.Context)  { contentLike(c, "post") }
func positiveID(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"message": "ID无效"})
		return 0, false
	}
	return id, true
}
func validText(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) > 0 && utf8.RuneCountInString(value) <= max
}

func FavoriteFolders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	actor, _ := middleware.GetUserID(c)
	if c.Request.Method == "GET" {
		items, err := repo.ListFavoriteFolders(c.Request.Context(), actor)
		if err != nil {
			interactionError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": items})
		return
	}
	if _, ok := commentActor(c); !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !commentBody(c, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !validText(req.Name, 60) {
		c.JSON(400, gin.H{"message": "收藏夹名称须为1–60字"})
		return
	}
	item, err := repo.CreateFavoriteFolder(c.Request.Context(), actor, req.Name)
	if err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(201, gin.H{"data": item})
}
func ChangeFavoriteFolder(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	id, ok := positiveID(c, "folder_id")
	if !ok {
		return
	}
	remove := c.Request.Method == "DELETE"
	name := ""
	if !remove {
		var req struct {
			Name string `json:"name"`
		}
		if !commentBody(c, &req) {
			return
		}
		name = strings.TrimSpace(req.Name)
		if !validText(name, 60) {
			c.JSON(400, gin.H{"message": "收藏夹名称须为1–60字"})
			return
		}
	}
	if err := repo.ChangeFavoriteFolder(c.Request.Context(), id, actor, name, remove); err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "收藏夹已更新"})
}
func FavoriteItems(c *gin.Context) {
	actor, _ := middleware.GetUserID(c)
	id, ok := positiveID(c, "folder_id")
	if !ok {
		return
	}
	before, limit, ok := commentAdminPage(c)
	if !ok {
		return
	}
	items, err := repo.ListFavorites(c.Request.Context(), id, actor, before, limit+1)
	if err != nil {
		interactionError(c, err)
		return
	}
	commentPageResult(c, items, limit, func(item model.FavoriteView) uint64 { return item.ID })
}
func SetFavorite(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	folder, ok := positiveID(c, "folder_id")
	if !ok {
		return
	}
	video, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	if err := repo.SetFavorite(c.Request.Context(), folder, video, actor, c.Request.Method == "PUT"); err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "收藏状态已更新"})
}
func FavoriteMembership(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	actor, _ := middleware.GetUserID(c)
	id, ok := positiveID(c, "video_id")
	if !ok {
		return
	}
	ids, err := repo.FavoriteMembership(c.Request.Context(), actor, id)
	if err != nil {
		interactionError(c, err)
		return
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.FormatUint(id, 10)
	}
	c.JSON(200, gin.H{"data": values})
}
func Notifications(c *gin.Context) {
	actor, _ := middleware.GetUserID(c)
	before, limit, ok := commentAdminPage(c)
	if !ok {
		return
	}
	items, err := repo.ListNotifications(c.Request.Context(), actor, before, limit+1)
	if err != nil {
		interactionError(c, err)
		return
	}
	commentPageResult(c, items, limit, func(item model.NotificationView) uint64 { return item.ID })
}
func UnreadNotifications(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	actor, _ := middleware.GetUserID(c)
	count, err := repo.UnreadNotifications(c.Request.Context(), actor)
	if err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"count": count}})
}
func ReadNotification(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	all := c.Param("notification_id") == ""
	var id uint64
	if all {
		var req struct {
			Before uint64 `json:"before,string"`
		}
		if !commentBody(c, &req) {
			return
		}
		id = req.Before
		if id == 0 {
			c.JSON(400, gin.H{"message": "缺少已查看的通知边界"})
			return
		}
	} else {
		id, ok = positiveID(c, "notification_id")
		if !ok {
			return
		}
	}
	if err := repo.ReadNotification(c.Request.Context(), actor, id, all); err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "已标记为已读"})
}

func validReportRef(kind, ref string) (string, bool) {
	if kind == "danmaku" {
		id, err := uuid.Parse(ref)
		return id.String(), err == nil
	}
	if kind != "video" && kind != "post" && kind != "comment" && kind != "video_danmaku" {
		return "", false
	}
	id, err := strconv.ParseUint(ref, 10, 64)
	return strconv.FormatUint(id, 10), err == nil && id > 0
}
func SubmitReport(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	var req struct {
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		Reason     string `json:"reason"`
	}
	if !commentBody(c, &req) {
		return
	}
	ref, ok := validReportRef(req.TargetType, req.TargetID)
	req.Reason = strings.TrimSpace(req.Reason)
	if !ok || !validText(req.Reason, 1000) {
		c.JSON(400, gin.H{"message": "举报目标或理由无效"})
		return
	}
	if !middleware.CheckCommunityLimit(c, "report", actor, req.TargetType+":"+ref) {
		return
	}
	if err := repo.SubmitReport(c.Request.Context(), req.TargetType, ref, actor, req.Reason); err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "举报已提交"})
}
func Reports(c *gin.Context) {
	before, limit, ok := commentAdminPage(c)
	if !ok {
		return
	}
	status := c.DefaultQuery("status", "pending")
	kind := c.Query("target_type")
	if (status != "pending" && status != "resolved" && status != "dismissed") || (kind != "" && kind != "video" && kind != "post" && kind != "comment" && kind != "danmaku" && kind != "video_danmaku") {
		c.JSON(400, gin.H{"message": "筛选条件无效"})
		return
	}
	items, err := repo.ListReports(c.Request.Context(), status, kind, before, limit+1)
	if err != nil {
		interactionError(c, err)
		return
	}
	commentPageResult(c, items, limit, func(item repo.ReportView) uint64 { return item.ID })
}
func ResolveReport(c *gin.Context) {
	actor, ok := commentActor(c)
	if !ok {
		return
	}
	id, ok := positiveID(c, "report_id")
	if !ok {
		return
	}
	var req struct {
		Action  string `json:"action"`
		Reason  string `json:"reason"`
		Version uint64 `json:"version,string"`
	}
	if !commentBody(c, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if !validText(req.Reason, 1000) || (req.Action != "resolved" && req.Action != "dismissed" && req.Action != "hidden" && req.Action != "restored") || ((req.Action == "hidden" || req.Action == "restored") && req.Version == 0) {
		c.JSON(400, gin.H{"message": "处理动作、理由或内容版本无效"})
		return
	}
	if err := repo.ResolveReport(c.Request.Context(), id, actor, req.Version, req.Action, req.Reason); err != nil {
		interactionError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "处理结果已保存"})
}
func ReportHistory(c *gin.Context) {
	id, ok := positiveID(c, "report_id")
	if !ok {
		return
	}
	before, limit, ok := commentAdminPage(c)
	if !ok {
		return
	}
	items, err := repo.ReportHistory(c.Request.Context(), id, before, limit+1)
	if err != nil {
		interactionError(c, err)
		return
	}
	commentPageResult(c, items, limit, func(item model.ReportAction) uint64 { return item.ID })
}
