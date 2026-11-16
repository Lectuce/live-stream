package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

var ErrWatchConflict = errors.New("watch session expired")
var ErrWatchInput = errors.New("invalid watch progress")

func watchTransaction(ctx context.Context, actor uint64, fn func(*gorm.DB, *model.WatchHistoryState) error) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state := model.WatchHistoryState{UserID: actor, HistoryEpoch: 1}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", actor).First(&state).Error; err != nil {
			return err
		}
		return fn(tx, &state)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func watchVideo(tx *gorm.DB, video uint64) (model.Video, error) {
	var v model.Video
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("video_id = ? AND publish_status='published' AND process_status='ready' AND audit_status='approved'", video).First(&v).Error
	return v, err
}

func watchRow(tx *gorm.DB, actor, video uint64) (model.WatchHistory, error) {
	row := model.WatchHistory{UserID: actor, VideoID: video}
	err := tx.Where("user_id = ? AND video_id = ?", actor, video).Limit(1).Find(&row).Error
	return row, err
}

func watchDTO(row model.WatchHistory, epoch, duration uint64) *model.WatchProgress {
	visible := row.HistoryEpoch == epoch && row.DeletedAt == nil && row.LastWatchedAt != nil
	position := uint64(0)
	if visible {
		position = min(row.PositionMS, duration)
	}
	return &model.WatchProgress{VideoID: row.VideoID, PositionMS: position, DurationMS: duration, HistoryEpoch: epoch, SessionVersion: row.SessionVersion, SessionID: row.SessionID, LastSeq: row.LastSeq, HasHistory: visible}
}

func GetWatchProgress(ctx context.Context, actor, video uint64) (*model.WatchProgress, error) {
	var result *model.WatchProgress
	err := watchTransaction(ctx, actor, func(tx *gorm.DB, state *model.WatchHistoryState) error {
		v, err := watchVideo(tx, video)
		if err != nil {
			return err
		}
		row, err := watchRow(tx, actor, video)
		if err != nil {
			return err
		}
		result = watchDTO(row, state.HistoryEpoch, v.DurationMS)
		return nil
	})
	return result, err
}

func StartWatchSession(ctx context.Context, actor, video uint64, req model.WatchSessionRequest) (*model.WatchProgress, error) {
	var result *model.WatchProgress
	err := watchTransaction(ctx, actor, func(tx *gorm.DB, state *model.WatchHistoryState) error {
		if state.HistoryEpoch != req.HistoryEpoch {
			return ErrWatchConflict
		}
		v, err := watchVideo(tx, video)
		if err != nil {
			return err
		}
		row, err := watchRow(tx, actor, video)
		if err != nil {
			return err
		}
		// Retrying an acknowledged-but-lost claim must not reset its sequence.
		if row.HistoryEpoch == state.HistoryEpoch && row.DeletedAt == nil && row.SessionID == req.SessionID && req.SessionVersion < row.SessionVersion && req.SessionVersion+1 == row.SessionVersion {
			result = watchDTO(row, state.HistoryEpoch, v.DurationMS)
			return nil
		}
		if req.SessionVersion != row.SessionVersion || req.SessionID == row.SessionID {
			return ErrWatchConflict
		}
		if row.HistoryEpoch != state.HistoryEpoch || row.DeletedAt != nil {
			row.PositionMS, row.LastWatchedAt, row.DeletedAt = 0, nil, nil
		}
		row.HistoryEpoch, row.SessionID, row.LastSeq = state.HistoryEpoch, req.SessionID, 0
		row.SessionVersion++
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		result = watchDTO(row, state.HistoryEpoch, v.DurationMS)
		return nil
	})
	return result, err
}

// The caller holds the per-user state lock until commit. Never upsert here:
// deleted/cleared records can only be restored by an explicit new session.
func validateWatchProgress(row model.WatchHistory, epoch, duration uint64, req model.WatchProgressRequest) (bool, error) {
	if row.ID == 0 || row.DeletedAt != nil || epoch != req.HistoryEpoch || row.HistoryEpoch != epoch || row.SessionID != req.SessionID || row.SessionVersion != req.SessionVersion {
		return false, ErrWatchConflict
	}
	if req.PositionMS == nil || duration == 0 || *req.PositionMS > duration || req.Seq == 0 || req.Seq > 9007199254740991 {
		return false, ErrWatchInput
	}
	if req.Seq == row.LastSeq && *req.PositionMS != row.PositionMS {
		return false, ErrWatchInput
	}
	return req.Seq > row.LastSeq, nil
}

func SaveWatchProgress(ctx context.Context, actor, video uint64, req model.WatchProgressRequest) error {
	return watchTransaction(ctx, actor, func(tx *gorm.DB, state *model.WatchHistoryState) error {
		row, err := watchRow(tx, actor, video)
		if err != nil {
			return err
		}
		v, err := watchVideo(tx, video)
		if err != nil {
			return err
		}
		update, err := validateWatchProgress(row, state.HistoryEpoch, v.DurationMS, req)
		if err != nil || !update {
			return err
		}
		return tx.Model(&model.WatchHistory{}).Where("id = ? AND user_id = ?", row.ID, actor).Updates(map[string]any{
			"position_ms": *req.PositionMS, "last_seq": req.Seq, "last_watched_at": time.Now().UTC(),
		}).Error
	})
}

func DeleteWatchHistory(ctx context.Context, actor, video uint64) error {
	return watchTransaction(ctx, actor, func(tx *gorm.DB, state *model.WatchHistoryState) error {
		if video == 0 {
			return tx.Model(state).Update("history_epoch", state.HistoryEpoch+1).Error
		}
		row, err := watchRow(tx, actor, video)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		row.DeletedAt, row.SessionID = &now, ""
		row.PositionMS, row.LastWatchedAt, row.LastSeq = 0, nil, 0
		row.SessionVersion++
		row.HistoryEpoch = state.HistoryEpoch
		return tx.Save(&row).Error
	})
}

func ListWatchHistory(ctx context.Context, actor, epoch uint64, before time.Time, id uint64, limit int) ([]model.WatchHistoryItem, uint64, error) {
	items := make([]model.WatchHistoryItem, 0)
	var current uint64
	err := watchTransaction(ctx, actor, func(tx *gorm.DB, state *model.WatchHistoryState) error {
		current = state.HistoryEpoch
		if epoch != 0 && epoch != current {
			return ErrWatchConflict
		}
		q := tx.Table("watch_history AS h").Select(`h.id, h.video_id, h.position_ms, h.last_watched_at,
COALESCE(v.title,'') AS title, COALESCE(v.cover_url,'') AS cover_url,
COALESCE(v.duration_ms,0) AS duration_ms, COALESCE(v.owner_id,0) AS owner_id,
COALESCE(u.user_name,'') AS owner_name, (v.video_id IS NOT NULL) AS available`).
			Joins("LEFT JOIN videos v ON v.video_id=h.video_id AND v.deleted_at IS NULL AND v.publish_status='published' AND v.process_status='ready' AND v.audit_status='approved'").
			Joins("LEFT JOIN users u ON u.user_id=v.owner_id").
			Where("h.user_id = ? AND h.history_epoch = ? AND h.deleted_at IS NULL AND h.last_watched_at IS NOT NULL", actor, current)
		if id != 0 {
			q = q.Where("(h.last_watched_at < ? OR (h.last_watched_at = ? AND h.id < ?))", before, before, id)
		}
		return q.Order("h.last_watched_at DESC, h.id DESC").Limit(limit).Scan(&items).Error
	})
	return items, current, err
}
