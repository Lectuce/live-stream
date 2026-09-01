package repo

import (
	"context"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"time"

	"gorm.io/gorm"
)

// PersistDanmaEvents is the durable-history consumer. Realtime delivery uses a
// separate Kafka consumer group, so MySQL latency cannot delay WebSocket
// viewers. A repeated MessageID is a successful no-op.
func PersistDanmaEvents(ctx context.Context, messages []model.Message) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, message := range messages {
			if message.Type != model.MsgTypeDanma {
				continue
			}

			var existing int64
			if err := tx.Model(&model.Danma{}).
				Where("message_id = ?", message.MessageID).
				Count(&existing).Error; err != nil {
				return fmt.Errorf("check existing danma %s: %w", message.MessageID, err)
			}
			if existing > 0 {
				continue
			}

			createdAt := time.Now()
			if message.CreatedAt != nil {
				createdAt = *message.CreatedAt
			}
			danma := model.Danma{
				MessageID: message.MessageID,
				RoomID:    message.RoomID,
				RoomSeq:   message.RoomSeq,
				UserID:    message.UserID,
				UserName:  message.UserName,
				Content:   message.Content,
				CreatedAt: createdAt,
			}
			if err := tx.Create(&danma).Error; err != nil {
				return fmt.Errorf("insert danma %s: %w", message.MessageID, err)
			}
		}
		return nil
	})
}

func SelectMessagesAfterSeq(ctx context.Context, roomID, afterSeq uint64, limit int) ([]model.Danma, error) {
	var messages []model.Danma
	err := dao.SC.Db.WithContext(ctx).
		Where("room_id = ? AND room_seq > ?", roomID, afterSeq).
		Order("room_seq ASC").
		Limit(limit).
		Find(&messages).Error
	if err != nil {
		return nil, fmt.Errorf("select messages after room sequence: %w", err)
	}
	return messages, nil
}
