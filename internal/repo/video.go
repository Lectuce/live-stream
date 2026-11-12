package repo

import (
	"context"
	"time"

	"live-stream/internal/dao"
	"live-stream/internal/model"
)

func CreateVideo(ctx context.Context, video *model.Video) error {
	return dao.SC.Db.WithContext(ctx).Create(video).Error
}

// AttachVideoMedia only updates an author's draft. A concurrent publish wins safely.
func AttachVideoMedia(ctx context.Context, id, ownerID uint64, filename, playURL string, duration uint64, audit string) (int64, error) {
	result := dao.SC.Db.WithContext(ctx).Model(&model.Video{}).
		Where("video_id = ? AND owner_id = ? AND publish_status = ? AND (media_file = '' OR media_file IS NULL)", id, ownerID, "draft").
		Updates(map[string]any{"media_file": filename, "play_url": playURL, "duration_ms": duration, "process_status": "ready", "audit_status": audit})
	return result.RowsAffected, result.Error
}

func SelectVideo(ctx context.Context, videoID uint64) (*model.Video, error) {
	var video model.Video

	err := dao.SC.Db.WithContext(ctx).
		Where("video_id = ?", videoID).
		First(&video).Error

	return &video, err
}

func SelectOwnerVideo(ctx context.Context, videoID, ownerID uint64) (*model.Video, error) {
	var video model.Video
	err := dao.SC.Db.WithContext(ctx).
		Where("video_id = ? AND owner_id = ?", videoID, ownerID).
		First(&video).Error
	return &video, err
}

func SelectOwnerVideos(ctx context.Context, ownerID, beforeID uint64, limit int) ([]model.Video, error) {
	videos := make([]model.Video, 0)
	query := dao.SC.Db.WithContext(ctx).Where("owner_id = ?", ownerID)
	if beforeID > 0 {
		query = query.Where("video_id < ?", beforeID)
	}
	err := query.Order("video_id DESC").Limit(limit).Find(&videos).Error
	return videos, err
}

func SelectPublishedVideos(
	ctx context.Context,
	beforeID uint64,
	limit int,
) ([]model.Video, error) {
	videos := make([]model.Video, 0)

	query := dao.SC.Db.WithContext(ctx).
		Where(
			"publish_status = ? AND process_status = ? AND audit_status = ?",
			"published", "ready", "approved",
		)

	if beforeID > 0 {
		query = query.Where("video_id < ?", beforeID)
	}

	err := query.
		Order("video_id DESC").
		Limit(limit).
		Find(&videos).Error

	return videos, err
}

func PublishVideo(
	ctx context.Context,
	videoID uint64,
	ownerID uint64,
) (int64, error) {
	now := time.Now()

	result := dao.SC.Db.WithContext(ctx).
		Model(&model.Video{}).
		Where(
			"video_id = ? AND owner_id = ? AND publish_status = ? "+
				"AND process_status = ? AND audit_status = ?",
			videoID, ownerID, "draft", "ready", "approved",
		).
		Updates(map[string]any{
			"publish_status": "published",
			"published_at":   now,
		})

	return result.RowsAffected, result.Error
}
