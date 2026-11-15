package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
)

func validCommentText(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) > 0 && utf8.RuneCountInString(value) <= max
}

func EditComment(ctx context.Context, id, actor uint64, req model.EditCommentRequest) error {
	req.Content = strings.TrimSpace(req.Content)
	if req.Version == 0 || !validCommentText(req.Content, 2000) {
		return ErrCommentInput
	}
	return repo.EditComment(ctx, id, actor, req)
}

func ReviewComment(ctx context.Context, id, actor uint64, req model.ReviewCommentRequest) error {
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Version == 0 || (req.Decision != "approved" && req.Decision != "rejected" && req.Decision != "hidden") ||
		(!validCommentText(req.Reason, 1000) && !(req.Decision == "approved" && req.Reason == "")) {
		return ErrCommentInput
	}
	return repo.ReviewComment(ctx, id, actor, req)
}

func ReportComment(ctx context.Context, id, actor uint64, reason string) error {
	reason = strings.TrimSpace(reason)
	if !validCommentText(reason, 1000) {
		return ErrCommentInput
	}
	return repo.ReportComment(ctx, id, actor, reason)
}

func ResolveCommentReport(ctx context.Context, id, actor uint64, status, reason string) error {
	reason = strings.TrimSpace(reason)
	if (status != "resolved" && status != "dismissed") || !validCommentText(reason, 1000) {
		return ErrCommentInput
	}
	return repo.ResolveCommentReport(ctx, id, actor, status, reason)
}

type hotCommentCursor struct {
	Version    int    `json:"v"`
	Sort       string `json:"sort"`
	TargetType string `json:"target_type"`
	TargetID   uint64 `json:"target_id,string"`
	RootID     uint64 `json:"root_id,string"`
	Snapshot   string `json:"snapshot"`
	Offset     int    `json:"offset"`
}

func decodeHotCommentCursor(text, kind string, targetID uint64) (hotCommentCursor, error) {
	var c hotCommentCursor
	if len(text) > 1024 {
		return c, ErrCommentCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, ErrCommentCursor
	}
	if c.Version != 3 || c.Sort != "hot" || c.TargetType != kind || c.TargetID != targetID || c.RootID != 0 || c.Offset < 0 || c.Offset > 500 {
		return c, ErrCommentCursor
	}
	if _, err := uuid.Parse(c.Snapshot); err != nil {
		return c, ErrCommentCursor
	}
	return c, nil
}

func GetCommentPage(ctx context.Context, kind string, targetID, rootID, viewer uint64, sort, value string, limit int) (*model.CommentPage, error) {
	if limit < 1 || limit > 50 {
		return nil, ErrCommentInput
	}
	var page *model.CommentPage
	var err error
	switch sort {
	case "latest":
		page, err = ListComments(ctx, kind, targetID, rootID, value, limit)
	case "hot":
		if rootID != 0 {
			return nil, ErrCommentInput
		}
		page, err = hotComments(ctx, kind, targetID, value, limit)
	default:
		return nil, ErrCommentInput
	}
	if err != nil {
		return nil, err
	}
	if err := repo.FillCommentLikes(ctx, page.Items, viewer); err != nil {
		return nil, err
	}
	return page, nil
}

func hotComments(ctx context.Context, kind string, targetID uint64, value string, limit int) (*model.CommentPage, error) {
	c := hotCommentCursor{Version: 3, Sort: "hot", TargetType: kind, TargetID: targetID}
	var snapshot dao.CommentSnapshot
	var err error
	if value == "" {
		ids, err := repo.RankedCommentIDs(ctx, kind, targetID, 501)
		if err != nil {
			return nil, err
		}
		snapshot = dao.CommentSnapshot{TargetType: kind, TargetID: targetID, IDs: ids, Truncated: len(ids) > 500}
		if len(ids) > 500 {
			snapshot.IDs = ids[:500]
		}
		c.Snapshot, err = dao.SaveCommentSnapshot(ctx, snapshot)
		if err != nil {
			return nil, err
		}
	} else {
		c, err = decodeHotCommentCursor(value, kind, targetID)
		if err != nil {
			return nil, err
		}
		snapshot, err = dao.ReadCommentSnapshot(ctx, c.Snapshot)
		if err != nil {
			return nil, err
		}
		if snapshot.TargetType != kind || snapshot.TargetID != targetID || len(snapshot.IDs) > 500 || c.Offset > len(snapshot.IDs) {
			return nil, ErrCommentCursor
		}
	}
	items, offset, err := scanHotComments(snapshot.IDs, c.Offset, limit, func(ids []uint64) ([]model.CommentView, error) {
		return repo.CommentsByIDs(ctx, kind, targetID, ids)
	})
	if err != nil {
		return nil, err
	}
	page := &model.CommentPage{Items: items, HasMore: offset < len(snapshot.IDs), Truncated: snapshot.Truncated}
	if page.HasMore {
		c.Offset = offset
		data, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return page, nil
}

// Scan only the bounded snapshot and advance by consumed candidates, not returned rows.
func scanHotComments(ids []uint64, offset, limit int, fetch func([]uint64) ([]model.CommentView, error)) ([]model.CommentView, int, error) {
	items := make([]model.CommentView, 0, limit)
	for len(items) < limit && offset < len(ids) {
		end := min(offset+limit-len(items), len(ids))
		batch := ids[offset:end]
		rows, err := fetch(batch)
		if err != nil {
			return nil, offset, err
		}
		byID := map[uint64]model.CommentView{}
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, id := range batch {
			if row, ok := byID[id]; ok {
				items = append(items, row)
			}
		}
		offset = end
	}
	return items, offset, nil
}
