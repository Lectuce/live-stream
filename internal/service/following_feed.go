package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"time"
)

var ErrFollowingCursor = errors.New("invalid following cursor")

type followingCursor struct {
	Version int       `json:"v"`
	Viewer  uint64    `json:"u,string"`
	Time    time.Time `json:"t"`
	ID      uint64    `json:"id,string"`
}

func decodeFollowingCursor(value string, viewer uint64) (followingCursor, error) {
	var cursor followingCursor
	if value == "" {
		return cursor, nil
	}
	if len(value) > 1024 {
		return cursor, ErrFollowingCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor, ErrFollowingCursor
	}
	if json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Viewer != viewer || cursor.ID == 0 || cursor.Time.IsZero() || cursor.Time.Year() < 1970 || cursor.Time.Year() > 9999 || cursor.Time.Nanosecond()%1000 != 0 {
		return cursor, ErrFollowingCursor
	}
	return cursor, nil
}

func GetFollowingFeed(ctx context.Context, viewer uint64, value string, limit int) (*model.FollowingFeedPage, error) {
	if viewer == 0 || limit < 1 || limit > 50 {
		return nil, ErrFollowingCursor
	}
	cursor, err := decodeFollowingCursor(value, viewer)
	if err != nil {
		return nil, err
	}
	rows, err := repo.SelectFollowing(ctx, viewer, cursor.Time, cursor.ID, limit+1)
	if err != nil {
		return nil, err
	}
	page := &model.FollowingFeedPage{HasMore: len(rows) > limit}
	if page.HasMore {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		payload, err := json.Marshal(followingCursor{Version: 1, Viewer: viewer, Time: last.PublishedAt, ID: last.ID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(payload)
	}
	page.Items, err = repo.AssembleFollowing(ctx, rows)
	return page, err
}
