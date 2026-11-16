package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"live-stream/internal/model"
	"live-stream/internal/repo"
)

var ErrWatchCursor = errors.New("invalid history cursor")

type watchCursor struct {
	Version int       `json:"v"`
	User    uint64    `json:"u"`
	Epoch   uint64    `json:"e"`
	Time    time.Time `json:"t"`
	ID      uint64    `json:"i"`
}

type WatchHistoryPage struct {
	Items      []model.WatchHistoryItem `json:"items"`
	NextCursor string                   `json:"next_cursor"`
	HasMore    bool                     `json:"has_more"`
}

func decodeWatchCursor(raw string, actor uint64) (watchCursor, error) {
	var c watchCursor
	if raw == "" {
		return c, nil
	}
	if len(raw) > 512 {
		return c, ErrWatchCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(b, &c) != nil || c.Version != 1 || c.User != actor || c.Epoch == 0 || c.ID == 0 || c.Time.IsZero() {
		return c, ErrWatchCursor
	}
	return c, nil
}

func GetWatchHistoryPage(ctx context.Context, actor uint64, raw string, limit int) (*WatchHistoryPage, error) {
	if limit < 1 || limit > 50 {
		return nil, ErrWatchCursor
	}
	c, err := decodeWatchCursor(raw, actor)
	if err != nil {
		return nil, err
	}
	items, epoch, err := repo.ListWatchHistory(ctx, actor, c.Epoch, c.Time, c.ID, limit+1)
	if err != nil {
		return nil, err
	}
	page := &WatchHistoryPage{Items: items, HasMore: len(items) > limit}
	if page.HasMore {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		b, _ := json.Marshal(watchCursor{Version: 1, User: actor, Epoch: epoch, Time: last.LastWatchedAt, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return page, nil
}
