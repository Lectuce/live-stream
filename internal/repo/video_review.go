package repo

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

func SelectReviewVideos(ctx context.Context, status string, beforeID uint64, limit int) ([]model.Video, error) {
	videos := make([]model.Video, 0)
	query := dao.SC.Db.WithContext(ctx).Where("audit_status = ? AND process_status = ? AND media_file <> ''", status, "ready")
	if status == "pending" {
		query = query.Where("publish_status = ?", "draft")
	}
	if beforeID > 0 {
		query = query.Where("video_id < ?", beforeID)
	}
	err := query.Order("video_id DESC").Limit(limit).Find(&videos).Error
	return videos, err
}

func SelectVideoAuthorNames(ctx context.Context, videos []model.Video) (map[uint64]string, error) {
	names := make(map[uint64]string)
	if len(videos) == 0 {
		return names, nil
	}
	ids := make([]uint64, 0, len(videos))
	for _, v := range videos {
		ids = append(ids, v.OwnerID)
	}
	var users []model.User
	err := dao.SC.Db.WithContext(ctx).Select("user_id", "user_name").Where("user_id IN ?", ids).Find(&users).Error
	for _, u := range users {
		names[u.UserID] = u.UserName
	}
	return names, err
}

func SelectVideoReviews(ctx context.Context, id uint64) ([]model.VideoReview, error) {
	rows := make([]model.VideoReview, 0)
	err := dao.SC.Db.WithContext(ctx).Where("video_id = ?", id).Order("review_id DESC").Limit(20).Find(&rows).Error
	return rows, err
}

// The guard runs under the same row lock used by author metadata edits.
func ReviewVideoTransaction(ctx context.Context, id, reviewerID uint64, req model.ReviewVideoRequest, guard func(*model.Video) error) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var video model.Video
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("video_id = ?", id).First(&video).Error; err != nil {
			return err
		}
		if err := guard(&video); err != nil {
			return err
		}
		if err := tx.Model(&video).Updates(map[string]any{"audit_status": req.Decision, "audit_reason": req.Reason}).Error; err != nil {
			return err
		}
		return tx.Create(&model.VideoReview{VideoID: id, ContentVersion: video.ContentVersion, ReviewerID: reviewerID, ReviewerType: "human", Decision: req.Decision, Reason: req.Reason}).Error
	})
}
