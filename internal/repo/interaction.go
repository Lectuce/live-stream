package repo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

var ErrFolderDefault = errors.New("default folder cannot be deleted")
var ErrFolderLimit = errors.New("at most 100 folders")

func ContentLikeState(ctx context.Context, kind string, id, viewer uint64) (*model.ContentLikeState, error) {
	db := dao.SC.Db.WithContext(ctx)
	if err := publicCommentTarget(db, kind, id, false); err != nil {
		return nil, err
	}
	var state model.ContentLikeState
	err := db.Raw(`SELECT COALESCE((SELECT like_count FROM content_stats WHERE target_type=? AND target_id=?),0) AS like_count,
 EXISTS(SELECT 1 FROM content_likes WHERE target_type=? AND target_id=? AND user_id=?) AS liked`, kind, id, kind, id, viewer).Scan(&state).Error
	return &state, err
}

func SetContentLike(ctx context.Context, kind string, id, actor uint64, liked bool) (*model.ContentLikeState, error) {
	state := &model.ContentLikeState{Liked: liked}
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := publicCommentTarget(tx, kind, id, true); err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.ContentStat{TargetType: kind, TargetID: id}).Error; err != nil {
			return err
		}
		var stat model.ContentStat
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("target_type = ? AND target_id = ?", kind, id).First(&stat).Error; err != nil {
			return err
		}
		var count int64
		q := tx.Model(&model.ContentLike{}).Where("target_type = ? AND target_id = ? AND user_id = ?", kind, id, actor)
		if err := q.Count(&count).Error; err != nil {
			return err
		}
		state.LikeCount = stat.LikeCount
		if liked && count == 0 {
			if err := tx.Create(&model.ContentLike{TargetType: kind, TargetID: id, UserID: actor}).Error; err != nil {
				return err
			}
			state.LikeCount++
		} else if !liked && count != 0 {
			if err := q.Delete(&model.ContentLike{}).Error; err != nil {
				return err
			}
			state.LikeCount = max(0, state.LikeCount-1)
		} else {
			return nil
		}
		return tx.Model(&stat).Update("like_count", state.LikeCount).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return state, err
}

func EnsureDefaultFolder(ctx context.Context, owner uint64) error {
	return dao.SC.Db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.FavoriteFolder{OwnerID: owner, Slot: "default", Name: "默认收藏夹", IsDefault: true}).Error
}
func ListFavoriteFolders(ctx context.Context, owner uint64) ([]model.FavoriteFolder, error) {
	if err := EnsureDefaultFolder(ctx, owner); err != nil {
		return nil, err
	}
	items := make([]model.FavoriteFolder, 0)
	err := dao.SC.Db.WithContext(ctx).Where("owner_id = ?", owner).Order("is_default DESC, id DESC").Limit(100).Find(&items).Error
	return items, err
}
func CreateFavoriteFolder(ctx context.Context, owner uint64, name string) (*model.FavoriteFolder, error) {
	var folder model.FavoriteFolder
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Select("user_id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", owner).First(&user).Error; err != nil {
			return err
		}
		// Reserve one slot for the default folder even before it is initialized.
		var count int64
		if err := tx.Model(&model.FavoriteFolder{}).Where("owner_id = ? AND is_default = false", owner).Count(&count).Error; err != nil {
			return err
		}
		if count >= 99 {
			return ErrFolderLimit
		}
		folder = model.FavoriteFolder{OwnerID: owner, Slot: uuid.NewString(), Name: name}
		return tx.Create(&folder).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return &folder, err
}
func lockFavoriteFolder(tx *gorm.DB, id, owner uint64) (*model.FavoriteFolder, error) {
	var folder model.FavoriteFolder
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_id = ?", id, owner).First(&folder).Error
	return &folder, err
}
func ChangeFavoriteFolder(ctx context.Context, id, owner uint64, name string, remove bool) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		folder, err := lockFavoriteFolder(tx, id, owner)
		if err != nil {
			return err
		}
		if !remove {
			return tx.Model(folder).Update("name", name).Error
		}
		if folder.IsDefault {
			return ErrFolderDefault
		}
		if err := tx.Where("folder_id = ?", id).Delete(&model.FavoriteItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(folder).Error
	})
}
func SetFavorite(ctx context.Context, folderID, videoID, owner uint64, save bool) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock video before folder; removing unavailable entries does not require a public video.
		if save {
			if err := publicCommentTarget(tx, "video", videoID, true); err != nil {
				return err
			}
		}
		if _, err := lockFavoriteFolder(tx, folderID, owner); err != nil {
			return err
		}
		if !save {
			return tx.Where("folder_id = ? AND video_id = ?", folderID, videoID).Delete(&model.FavoriteItem{}).Error
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.FavoriteItem{FolderID: folderID, VideoID: videoID}).Error
	})
}
func FavoriteMembership(ctx context.Context, owner, videoID uint64) ([]uint64, error) {
	ids := make([]uint64, 0)
	err := dao.SC.Db.WithContext(ctx).Table("favorite_items AS i").Joins("JOIN favorite_folders AS f ON f.id = i.folder_id AND f.owner_id = ?", owner).Where("i.video_id = ?", videoID).Pluck("i.folder_id", &ids).Error
	return ids, err
}
func ListFavorites(ctx context.Context, folderID, owner, before uint64, limit int) ([]model.FavoriteView, error) {
	db := dao.SC.Db.WithContext(ctx)
	var folder model.FavoriteFolder
	if err := db.Where("id = ? AND owner_id = ?", folderID, owner).First(&folder).Error; err != nil {
		return nil, err
	}
	items := make([]model.FavoriteView, 0)
	q := db.Table("favorite_items AS i").Select("i.*, COALESCE(v.title,'视频不可用') AS title, COALESCE(v.cover_url,'') AS cover_url, v.video_id IS NOT NULL AS available").
		Joins("JOIN favorite_folders AS f ON f.id = i.folder_id AND f.owner_id = ?", owner).
		Joins("LEFT JOIN videos AS v ON v.video_id = i.video_id AND v.deleted_at IS NULL AND v.publish_status = 'published' AND v.process_status = 'ready' AND v.audit_status = 'approved'").Where("i.folder_id = ?", folderID)
	if before > 0 {
		q = q.Where("i.id < ?", before)
	}
	err := q.Order("i.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}
