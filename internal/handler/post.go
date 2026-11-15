package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/repo"
)

func postError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(404, gin.H{"message": "图文不存在或不可见"})
	case errors.Is(err, repo.ErrCommentClosed):
		c.JSON(409, gin.H{"message": "图文已被管理员隐藏，无法自行发布"})
	case errors.Is(err, repo.ErrPostConflict):
		c.JSON(409, gin.H{"message": "图文已在其他页面修改，请重新加载"})
	case errors.Is(err, repo.ErrPostImages):
		c.JSON(400, gin.H{"message": "图片不存在、不属于当前账号或已用于其他图文"})
	case errors.Is(err, repo.ErrEmptyPost):
		c.JSON(400, gin.H{"message": "请填写正文或添加图片"})
	default:
		log.Printf("post request: %v", err)
		c.JSON(500, gin.H{"message": "图文操作失败"})
	}
}

func postIdentity(c *gin.Context) (uint64, bool) {
	c.Header("Cache-Control", "private, no-store")
	uid, ok := middleware.GetUserID(c)
	if !ok || uid == 0 {
		c.JSON(401, gin.H{"message": "未登录"})
		return 0, false
	}
	if c.Request.Method != http.MethodGet && !trustedUploadOrigin(c) {
		c.JSON(403, gin.H{"message": "操作来源不允许"})
		return 0, false
	}
	return uid, true
}

func postID(c *gin.Context) (uint64, bool) {
	c.Header("Cache-Control", "private, no-store")
	id, err := strconv.ParseUint(c.Param("post_id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"message": "图文ID无效"})
		return 0, false
	}
	return id, true
}

func decodePostJSON(c *gin.Context, value any) bool {
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		c.JSON(400, gin.H{"message": "请求参数无效"})
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		c.JSON(400, gin.H{"message": "请求只能包含一个JSON对象"})
		return false
	}
	return true
}

func CreatePost(c *gin.Context) {
	uid, ok := postIdentity(c)
	if !ok {
		return
	}
	p, err := repo.CreatePost(c.Request.Context(), uid)
	if err != nil {
		postError(c, err)
		return
	}
	c.JSON(201, gin.H{"data": p, "message": "草稿已创建"})
}

func GetPost(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}
	p, err := repo.GetPost(c.Request.Context(), id, 0)
	if err != nil {
		postError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": p, "message": "success"})
}

func GetMyPost(c *gin.Context) {
	uid, ok := postIdentity(c)
	if !ok {
		return
	}
	id, ok := postID(c)
	if !ok {
		return
	}
	p, err := repo.GetPost(c.Request.Context(), id, uid)
	if err != nil {
		postError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": p, "message": "success"})
}

func ListMyPosts(c *gin.Context) {
	uid, ok := postIdentity(c)
	if !ok {
		return
	}
	listPosts(c, uid, false)
}

func ListAuthorPosts(c *gin.Context) {
	id, ok := parsePublicUserID(c)
	if !ok {
		return
	}
	if _, err := repo.SelectPublicUser(c.Request.Context(), id); err != nil {
		userFollowError(c, err)
		return
	}
	listPosts(c, id, true)
}

func listPosts(c *gin.Context, owner uint64, public bool) {
	before, limit, ok := parseVideoPage(c)
	if !ok {
		return
	}
	items, err := repo.ListPosts(c.Request.Context(), owner, before, limit+1, public)
	if err != nil {
		postError(c, err)
		return
	}
	hasMore := len(items) > limit
	cursor := ""
	if hasMore {
		items = items[:limit]
		cursor = strconv.FormatUint(items[len(items)-1].PostID, 10)
	}
	c.JSON(200, gin.H{"data": gin.H{"items": items, "next_cursor": cursor, "has_more": hasMore}, "message": "success"})
}

func EditPost(c *gin.Context) {
	uid, ok := postIdentity(c)
	if !ok {
		return
	}
	id, ok := postID(c)
	if !ok {
		return
	}
	var req model.EditPostRequest
	if !decodePostJSON(c, &req) {
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Version == 0 || utf8.RuneCountInString(req.Content) > 5000 || len(req.ImageIDs) > 9 {
		c.JSON(400, gin.H{"message": "正文最多5000字、图片最多9张，版本号必填"})
		return
	}
	seen := make(map[string]bool)
	for _, imageID := range req.ImageIDs {
		if _, err := uuid.Parse(imageID); err != nil || seen[imageID] {
			c.JSON(400, gin.H{"message": "图片ID无效或重复"})
			return
		}
		seen[imageID] = true
	}
	if err := repo.EditPost(c.Request.Context(), id, uid, req); err != nil {
		postError(c, err)
		return
	}
	GetMyPost(c)
}

func PublishPost(c *gin.Context) {
	uid, ok := postIdentity(c)
	if !ok {
		return
	}
	id, ok := postID(c)
	if !ok {
		return
	}
	var req struct {
		Version uint64 `json:"version,string"`
	}
	if !decodePostJSON(c, &req) {
		return
	}
	if req.Version == 0 {
		c.JSON(400, gin.H{"message": "版本号必填"})
		return
	}
	if err := repo.PublishPost(c.Request.Context(), id, uid, req.Version); err != nil {
		postError(c, err)
		return
	}
	GetMyPost(c)
}

func DeletePost(c *gin.Context) {
	uid, ok := postIdentity(c)
	if !ok {
		return
	}
	id, ok := postID(c)
	if !ok {
		return
	}
	if err := repo.DeletePost(c.Request.Context(), id, uid); err != nil {
		postError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "图文已删除"})
}
