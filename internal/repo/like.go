package repo

import (
	"context"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ApplyLikeEvent stores the durable idempotency record and increments the
// authoritative MySQL room counter in one transaction. Replaying an existing
// message_id is a successful no-op and returns the current counter.
func ApplyLikeEvent(ctx context.Context, message model.Message) (count int64, applied bool, err error) {
	err = dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		createdAt := time.Now()
		if message.CreatedAt != nil {
			createdAt = *message.CreatedAt
		}
		event := model.LikeEvent{
			MessageID: message.MessageID,
			RoomID:    message.RoomID,
			UserID:    message.UserID,
			CreatedAt: createdAt,
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
		if result.Error != nil {
			return fmt.Errorf("insert like event %s: %w", message.MessageID, result.Error)
		}
		applied = result.RowsAffected == 1
		if applied {
			result = tx.Model(&model.Room{}).
				Where("room_id = ?", message.RoomID).
				UpdateColumn("like_count", gorm.Expr("like_count + 1"))
			if result.Error != nil {
				return fmt.Errorf("increment room %d like count: %w", message.RoomID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("increment room %d like count: room not found", message.RoomID)
			}
		}
		var room model.Room
		if err := tx.Select("room_id", "like_count").
			Where("room_id = ?", message.RoomID).
			Take(&room).Error; err != nil {
			return fmt.Errorf("read room %d like count: %w", message.RoomID, err)
		}
		count = room.LikeCount
		return nil
	})
	return count, applied, err
}
