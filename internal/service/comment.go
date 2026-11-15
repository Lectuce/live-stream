package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"live-stream/internal/model"
	"live-stream/internal/repo"
)

var ErrCommentInput = errors.New("invalid comment input")
var ErrCommentCursor = errors.New("invalid comment cursor")

type commentCursor struct {
	Version    int       `json:"v"`
	Sort       string    `json:"sort"`
	TargetType string    `json:"target_type"`
	TargetID   uint64    `json:"target_id,string"`
	RootID     uint64    `json:"root_id,string"`
	Time       time.Time `json:"t"`
	ID         uint64    `json:"id,string"`
}

func normalizeComment(req model.CreateCommentRequest) (model.CreateCommentRequest, error) {
	req.Content = strings.TrimSpace(req.Content)
	if !utf8.ValidString(req.Content) || utf8.RuneCountInString(req.Content) < 1 || utf8.RuneCountInString(req.Content) > 2000 {
		return req, ErrCommentInput
	}
	id, err := uuid.Parse(req.RequestID)
	if err != nil {
		return req, ErrCommentInput
	}
	req.RequestID = id.String()
	return req, nil
}

func CreateComment(ctx context.Context, targetType string, targetID, userID uint64, req model.CreateCommentRequest) (*model.Comment, error) {
	req, err := normalizeComment(req)
	if err != nil {
		return nil, err
	}
	return repo.CreateComment(ctx, targetType, targetID, userID, req)
}

func decodeCommentCursor(value string, targetType string, targetID, rootID uint64) (commentCursor, error) {
	var cursor commentCursor
	if value == "" {
		return cursor, nil
	}
	if len(value) > 1024 {
		return cursor, ErrCommentCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor, ErrCommentCursor
	}
	if json.Unmarshal(data, &cursor) != nil || cursor.Version != 3 || cursor.Sort != "latest" || cursor.TargetType != targetType || cursor.TargetID != targetID || cursor.RootID != rootID || cursor.ID == 0 || cursor.Time.IsZero() || cursor.Time.Year() < 1970 || cursor.Time.Year() > 9999 || cursor.Time.Nanosecond()%1000 != 0 {
		return cursor, ErrCommentCursor
	}
	return cursor, nil
}

func ListComments(ctx context.Context, targetType string, targetID, rootID uint64, value string, limit int) (*model.CommentPage, error) {
	if limit < 1 || limit > 50 {
		return nil, ErrCommentInput
	}
	cursor, err := decodeCommentCursor(value, targetType, targetID, rootID)
	if err != nil {
		return nil, err
	}
	items, err := repo.ListComments(ctx, targetType, targetID, rootID, cursor.Time, cursor.ID, limit+1)
	if err != nil {
		return nil, err
	}
	page := &model.CommentPage{Items: items, HasMore: len(items) > limit}
	if page.HasMore {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		data, err := json.Marshal(commentCursor{Version: 3, Sort: "latest", TargetType: targetType, TargetID: targetID, RootID: rootID, Time: last.CreatedAt, ID: last.ID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return page, nil
}

func CreateVideoComment(ctx context.Context, videoID, userID uint64, req model.CreateCommentRequest) (*model.Comment, error) {
	return CreateComment(ctx, model.CommentTargetVideo, videoID, userID, req)
}
func ListVideoComments(ctx context.Context, videoID, rootID uint64, value string, limit int) (*model.CommentPage, error) {
	return ListComments(ctx, model.CommentTargetVideo, videoID, rootID, value, limit)
}
