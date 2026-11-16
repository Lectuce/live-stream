package repo

import (
	"context"
	"database/sql"
	"errors"
	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"time"
)

var ErrDanmakuPosition = errors.New("position outside video duration")

func CreateVideoDanmaku(ctx context.Context, item *model.VideoDanmaku) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var v model.Video
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("video_id = ? AND publish_status='published' AND process_status='ready' AND audit_status='approved'", item.VideoID).First(&v).Error; err != nil {
			return err
		}
		if v.DurationMS == 0 || item.PositionMS >= v.DurationMS {
			return ErrDanmakuPosition
		}
		var old model.VideoDanmaku
		result := tx.Where("user_id = ? AND request_id = ?", item.UserID, item.RequestID).Limit(1).Find(&old)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			if old.VideoID != item.VideoID || old.PositionMS != item.PositionMS || old.Content != item.Content || old.Color != item.Color {
				return ErrCommentRequest
			}
			if old.Hidden || old.DeletedAt != nil {
				return ErrCommentClosed
			}
			*item = old
			return nil
		}
		err := tx.Create(item).Error
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return ErrCommentRequest
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func ListVideoDanmakus(ctx context.Context, video, from, to, position, id uint64, limit int) ([]model.VideoDanmaku, error) {
	db := dao.SC.Db.WithContext(ctx)
	if err := publicCommentTarget(db, "video", video, false); err != nil {
		return nil, err
	}
	rows := make([]model.VideoDanmaku, 0)
	// Scan a bounded number of indexed candidates, including tombstones. The handler
	// filters them after advancing the cursor so hidden-heavy windows stay bounded.
	q := db.Where("video_id = ? AND position_ms >= ? AND position_ms < ?", video, from, to)
	if id > 0 {
		q = q.Where("(position_ms > ? OR (position_ms = ? AND id > ?))", position, position, id)
	}
	err := q.Order("position_ms ASC, id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func DeleteVideoDanmaku(ctx context.Context, video, id, actor uint64) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var d model.VideoDanmaku
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND video_id = ? AND user_id = ?", id, video, actor).First(&d).Error; err != nil {
			return err
		}
		if d.DeletedAt != nil {
			return nil
		}
		return tx.Model(&d).Updates(map[string]any{"deleted_at": time.Now().UTC(), "version": gorm.Expr("version + 1")}).Error
	})
}
