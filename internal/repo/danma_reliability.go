package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PersistDanmaEvents writes each new danma and its outbox event in the same
// transaction. A repeated MessageID is a successful no-op, which makes Kafka
// redelivery safe.
func PersistDanmaEvents(ctx context.Context, messages []model.Message) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sequences := make(map[uint64]*model.RoomMessageSequence)

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

			sequence, ok := sequences[message.RoomID]
			if !ok {
				seed := model.RoomMessageSequence{RoomID: message.RoomID}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
					return fmt.Errorf("create room sequence %d: %w", message.RoomID, err)
				}
				sequence = &model.RoomMessageSequence{}
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
					Where("room_id = ?", message.RoomID).
					First(sequence).Error; err != nil {
					return fmt.Errorf("lock room sequence %d: %w", message.RoomID, err)
				}
				sequences[message.RoomID] = sequence
			}

			sequence.LastSeq++
			message.RoomSeq = sequence.LastSeq
			danma := model.Danma{
				MessageID: message.MessageID,
				RoomID:    message.RoomID,
				RoomSeq:   message.RoomSeq,
				UserID:    message.UserID,
				UserName:  message.UserName,
				Content:   message.Content,
			}
			if err := tx.Create(&danma).Error; err != nil {
				return fmt.Errorf("insert danma %s: %w", message.MessageID, err)
			}
			message.ID = danma.ID
			message.CreatedAt = &danma.CreatedAt

			payload, err := json.Marshal(message)
			if err != nil {
				return fmt.Errorf("marshal outbox event %s: %w", message.MessageID, err)
			}
			outbox := model.DanmaOutbox{
				MessageID:     message.MessageID,
				RoomID:        message.RoomID,
				RoomSeq:       message.RoomSeq,
				Payload:       payload,
				NextAttemptAt: time.Now(),
			}
			if err := tx.Create(&outbox).Error; err != nil {
				return fmt.Errorf("insert outbox event %s: %w", message.MessageID, err)
			}
		}

		for _, sequence := range sequences {
			if err := tx.Model(&model.RoomMessageSequence{}).
				Where("room_id = ?", sequence.RoomID).
				Updates(map[string]any{
					"last_seq":   sequence.LastSeq,
					"updated_at": time.Now(),
				}).Error; err != nil {
				return fmt.Errorf("update room sequence %d: %w", sequence.RoomID, err)
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

func SelectPendingOutbox(ctx context.Context, limit int) ([]model.DanmaOutbox, error) {
	var items []model.DanmaOutbox
	err := dao.SC.Db.WithContext(ctx).
		Where("published_at IS NULL").
		Order("id ASC").
		Limit(limit).
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("select pending outbox: %w", err)
	}
	return items, nil
}

func MarkOutboxPublished(ctx context.Context, id uint64) error {
	now := time.Now()
	return dao.SC.Db.WithContext(ctx).
		Model(&model.DanmaOutbox{}).
		Where("id = ? AND published_at IS NULL", id).
		Updates(map[string]any{"published_at": &now, "updated_at": now}).Error
}

func MarkOutboxRetry(ctx context.Context, id uint64, retryAt time.Time) error {
	return dao.SC.Db.WithContext(ctx).
		Model(&model.DanmaOutbox{}).
		Where("id = ? AND published_at IS NULL", id).
		Updates(map[string]any{
			"attempts":        gorm.Expr("attempts + 1"),
			"next_attempt_at": retryAt,
			"updated_at":      time.Now(),
		}).Error
}
