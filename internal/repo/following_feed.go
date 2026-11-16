package repo

import (
	"context"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"time"
)

type FollowingRow struct {
	LiveRoomID      uint64
	LiveTitle       string
	LiveStatus      string
	ID              uint64
	EventID         string
	EventType       string
	ActorID         uint64
	TargetID        uint64
	PublishedAt     time.Time
	UserName        string
	AvatarURL       string
	VideoTitle      string
	CoverURL        string
	DurationMS      uint64
	PostContent     string
	PostVersion     uint64
	PostCreatedAt   *time.Time
	PostUpdatedAt   *time.Time
	PostPublishedAt *time.Time
}

func SelectFollowing(ctx context.Context, viewer uint64, before time.Time, beforeID uint64, limit int) ([]FollowingRow, error) {
	rows := make([]FollowingRow, 0)
	q := dao.SC.Db.WithContext(ctx).Table("feed_events AS e").
		Select(`e.id,e.event_id,e.event_type,e.actor_id,e.target_id,e.published_at,u.user_name,u.avatar_url,
		v.title AS video_title,v.cover_url,v.duration_ms,p.content AS post_content,p.version AS post_version,
		p.created_at AS post_created_at,p.updated_at AS post_updated_at,p.published_at AS post_published_at,
        s.room_id AS live_room_id,s.title AS live_title,s.status AS live_status`).
		Joins("JOIN user_follows AS f ON f.followee_id = e.actor_id AND f.follower_id = ?", viewer).
		Joins("JOIN users AS u ON u.user_id = e.actor_id").
		Joins("LEFT JOIN videos AS v ON e.event_type = 'video.published' AND v.video_id = e.target_id AND v.owner_id = e.actor_id").
		Joins("LEFT JOIN posts AS p ON e.event_type = 'post.published' AND p.post_id = e.target_id AND p.owner_id = e.actor_id").
		Joins("LEFT JOIN live_sessions AS s ON e.event_type = 'live.started' AND s.id = e.target_id AND s.owner_id = e.actor_id").
		Where(`(e.event_type = 'video.published' AND v.publish_status = 'published' AND v.process_status = 'ready' AND v.audit_status = 'approved' AND v.deleted_at IS NULL AND v.video_id IS NOT NULL)
		OR (e.event_type = 'post.published' AND p.status = 'published' AND p.deleted_at IS NULL AND p.post_id IS NOT NULL)
        OR (e.event_type = 'live.started' AND s.started_at IS NOT NULL)`)
	if beforeID > 0 {
		q = q.Where("(e.published_at < ? OR (e.published_at = ? AND e.id < ?))", before, before, beforeID)
	}
	err := q.Order("e.published_at DESC, e.id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func AssembleFollowing(ctx context.Context, rows []FollowingRow) ([]model.FollowingFeedItem, error) {
	ids := make([]uint64, 0)
	for _, row := range rows {
		if row.EventType == "post.published" {
			ids = append(ids, row.TargetID)
		}
	}
	images, err := PostImages(ctx, ids)
	if err != nil {
		return nil, err
	}
	items := make([]model.FollowingFeedItem, 0, len(rows))
	for _, row := range rows {
		item := model.FollowingFeedItem{EventID: row.EventID, Type: row.EventType, PublishedAt: row.PublishedAt, Author: model.FeedAuthor{UserID: row.ActorID, UserName: row.UserName, AvatarURL: row.AvatarURL}}
		if row.EventType == "video.published" {
			item.Video = &model.FeedVideo{VideoID: row.TargetID, Title: row.VideoTitle, CoverURL: row.CoverURL, DurationMS: row.DurationMS}
		} else if row.EventType == "live.started" {
			item.Live = &model.FeedLive{SessionID: row.TargetID, RoomID: row.LiveRoomID, Title: row.LiveTitle, Status: row.LiveStatus}
		} else {
			list := images[row.TargetID]
			if list == nil {
				list = []model.PostImageView{}
			}
			post := model.Post{PostID: row.TargetID, OwnerID: row.ActorID, Content: row.PostContent, Status: "published", Version: row.PostVersion, PublishedAt: row.PostPublishedAt}
			if row.PostCreatedAt != nil {
				post.CreatedAt = *row.PostCreatedAt
			}
			if row.PostUpdatedAt != nil {
				post.UpdatedAt = *row.PostUpdatedAt
			}
			item.Post = &model.PostView{Post: post, Images: list}
		}
		items = append(items, item)
	}
	return items, nil
}
