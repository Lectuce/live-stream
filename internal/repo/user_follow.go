package repo

import (
	"context"

	"live-stream/internal/dao"
	"live-stream/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func SelectPublicUser(ctx context.Context, id uint64) (*model.PublicUser, error) {
	var user model.PublicUser
	err := dao.SC.Db.WithContext(ctx).Model(&model.User{}).
		Select("user_id, user_name, avatar_url, bio").Where("user_id = ?", id).Take(&user).Error
	return &user, err
}

func publicAuthorVideos(db *gorm.DB, ownerID uint64) *gorm.DB {
	return db.Model(&model.Video{}).Where("owner_id = ? AND publish_status = ? AND process_status = ? AND audit_status = ?",
		ownerID, "published", "ready", "approved")
}

func PublicUserCounts(ctx context.Context, user *model.PublicUser) error {
	db := dao.SC.Db.WithContext(ctx)
	if err := db.Model(&model.UserFollow{}).Where("followee_id = ?", user.UserID).Count(&user.FollowerCount).Error; err != nil {
		return err
	}
	if err := db.Model(&model.UserFollow{}).Where("follower_id = ?", user.UserID).Count(&user.FollowingCount).Error; err != nil {
		return err
	}
	return publicAuthorVideos(db, user.UserID).Count(&user.VideoCount).Error
}

func SelectPublicAuthorVideos(ctx context.Context, ownerID, beforeID uint64, limit int) ([]model.Video, error) {
	videos := make([]model.Video, 0)
	query := publicAuthorVideos(dao.SC.Db.WithContext(ctx), ownerID)
	if beforeID > 0 {
		query = query.Where("video_id < ?", beforeID)
	}
	err := query.Order("video_id DESC").Limit(limit).Find(&videos).Error
	return videos, err
}

func SetUserFollow(ctx context.Context, followerID, followeeID uint64, follow bool) error {
	db := dao.SC.Db.WithContext(ctx)
	if !follow {
		return db.Where("follower_id = ? AND followee_id = ?", followerID, followeeID).Delete(&model.UserFollow{}).Error
	}
	// MySQL uses a no-op ON DUPLICATE KEY UPDATE; retries never create a second relation.
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.UserFollow{
		FollowerID: followerID, FolloweeID: followeeID,
	}).Error
}

func SelectFollowState(ctx context.Context, followerID, followeeID uint64) (*model.FollowState, error) {
	var state model.FollowState
	// One statement gives the relation and total a consistent database snapshot.
	err := dao.SC.Db.WithContext(ctx).Raw(`SELECT
		EXISTS(SELECT 1 FROM user_follows WHERE follower_id = ? AND followee_id = ?) AS following,
		(SELECT COUNT(*) FROM user_follows WHERE followee_id = ?) AS follower_count`,
		followerID, followeeID, followeeID).Scan(&state).Error
	return &state, err
}
