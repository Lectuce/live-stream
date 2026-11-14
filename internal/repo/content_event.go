package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

func QueueContentEvent(tx *gorm.DB, kind string, actor, target uint64, published time.Time) error {
	item := model.OutboxEvent{ContentEvent: model.ContentEvent{
		EventID: uuid.NewString(), BusinessKey: fmt.Sprintf("%s:%d", kind, target),
		ActorID: actor, TargetID: target, EventType: kind, PublishedAt: published.UTC().Truncate(time.Microsecond),
	}, Status: "pending", NextRetryAt: time.Now().UTC()}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error
}

func ClaimContentEvent(ctx context.Context) (*model.OutboxEvent, error) {
	var event model.OutboxEvent
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("(status = 'pending' AND next_retry_at <= ?) OR (status = 'sending' AND locked_until <= ?)", now, now).
			Order("id ASC").First(&event).Error; err != nil {
			return err
		}
		event.LockToken = uuid.NewString()
		return tx.Model(&event).Updates(map[string]any{"status": "sending", "lock_token": event.LockToken,
			"locked_until": now.Add(time.Minute), "attempts": gorm.Expr("attempts + 1")}).Error
	})
	return &event, err
}

func FinishContentEvent(ctx context.Context, event *model.OutboxEvent, deliveryErr error) error {
	updates := map[string]any{"status": "sent", "sent_at": time.Now(), "locked_until": nil, "lock_token": "", "last_error": ""}
	if deliveryErr != nil {
		delay := 1 << min(event.Attempts+1, 8)
		message := []rune(deliveryErr.Error())
		if len(message) > 1000 {
			message = message[:1000]
		}
		updates["status"], updates["last_error"], updates["sent_at"] = "pending", string(message), nil
		updates["next_retry_at"] = time.Now().Add(time.Duration(delay) * time.Second)
	}
	return dao.SC.Db.WithContext(ctx).Model(&model.OutboxEvent{}).
		Where("id = ? AND status = 'sending' AND lock_token = ?", event.ID, event.LockToken).Updates(updates).Error
}

func ProjectContentEvent(ctx context.Context, event model.ContentEvent) error {
	return dao.SC.Db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.FeedEvent{ContentEvent: event}).Error
}

// Backfill one bounded page; business keys make retries safe alongside live publishing.
func BackfillVideoEvents(ctx context.Context, after uint64) (uint64, int, error) {
	var videos []model.Video
	if err := dao.SC.Db.WithContext(ctx).Where("video_id > ? AND publish_status = ? AND process_status = ? AND audit_status = ?", after, "published", "ready", "approved").Order("video_id ASC").Limit(100).Find(&videos).Error; err != nil {
		return after, 0, err
	}
	for _, video := range videos {
		err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current model.Video
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("video_id = ?", video.VideoID).First(&current).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					return nil
				}
				return err
			}
			if current.PublishStatus != "published" || current.ProcessStatus != "ready" || current.AuditStatus != "approved" {
				return nil
			}
			published := current.CreatedAt
			if current.PublishedAt != nil {
				published = *current.PublishedAt
			}
			return QueueContentEvent(tx, "video.published", current.OwnerID, current.VideoID, published)
		})
		if err != nil {
			return after, 0, err
		}
		after = video.VideoID
	}
	return after, len(videos), nil
}
