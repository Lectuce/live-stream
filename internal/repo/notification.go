package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

// Only references are retained. Hidden/deleted text can never leak through an old notification payload.
func QueueNotification(tx *gorm.DB, key, kind, targetType string, target, actor, recipient uint64) error {
	if recipient == 0 || recipient == actor {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.NotificationOutbox{NotificationData: model.NotificationData{EventKey: key, Kind: kind, TargetType: targetType, TargetID: target, ActorID: actor, RecipientID: recipient, CreatedAt: time.Now().UTC()}}).Error
}

// The inbox and outbox share MySQL: delivery and acknowledgement fit in one short transaction.
// On crash either both commit or both roll back; SKIP LOCKED allows multiple workers safely.
func DeliverNotifications(ctx context.Context) (int, error) {
	count := 0
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.NotificationOutbox
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("delivered_at IS NULL").Order("id ASC").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.Notification{NotificationData: row.NotificationData}).Error; err != nil {
				return err
			}
			if err := tx.Model(&row).Update("delivered_at", time.Now().UTC()).Error; err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func ListNotifications(ctx context.Context, recipient, before uint64, limit int) ([]model.NotificationView, error) {
	items := make([]model.NotificationView, 0)
	q := dao.SC.Db.WithContext(ctx).Table("notifications AS n").Select(`n.*, COALESCE(u.user_name,'已注销用户') AS actor_name,
 CASE
 WHEN n.target_type='user' AND tu.user_id IS NOT NULL THEN CONCAT('/space/',tu.user_id)
 WHEN n.target_type='video' AND v.owner_id=n.recipient_id AND v.deleted_at IS NULL THEN '/studio'
 WHEN n.target_type='comment' AND c.deleted_at IS NULL
 AND ((c.audit_status='approved' AND (c.root_id=0 OR root.audit_status='approved')) OR (n.kind='comment.review' AND c.user_id=n.recipient_id))
 AND cv.video_id IS NOT NULL THEN CONCAT('/video/',cv.video_id)
 WHEN n.target_type='comment' AND c.deleted_at IS NULL
 AND ((c.audit_status='approved' AND (c.root_id=0 OR root.audit_status='approved')) OR (n.kind='comment.review' AND c.user_id=n.recipient_id))
 AND cp.post_id IS NOT NULL THEN CONCAT('/posts/',cp.post_id)
 ELSE '' END AS url`).
		Joins("LEFT JOIN users u ON u.user_id=n.actor_id").Joins("LEFT JOIN users tu ON n.target_type='user' AND tu.user_id=n.target_id").
		Joins("LEFT JOIN videos v ON n.target_type='video' AND v.video_id=n.target_id").
		Joins("LEFT JOIN comments c ON n.target_type='comment' AND c.id=n.target_id").Joins("LEFT JOIN comments root ON root.id=c.root_id AND root.target_type=c.target_type AND root.target_id=c.target_id").
		Joins("LEFT JOIN videos cv ON c.target_type='video' AND cv.video_id=c.target_id AND cv.deleted_at IS NULL AND cv.publish_status='published' AND cv.process_status='ready' AND cv.audit_status='approved'").
		Joins("LEFT JOIN posts cp ON c.target_type='post' AND cp.post_id=c.target_id AND cp.deleted_at IS NULL AND cp.status='published'").Where("n.recipient_id = ?", recipient)
	if before > 0 {
		q = q.Where("n.id < ?", before)
	}
	err := q.Order("n.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}
func UnreadNotifications(ctx context.Context, recipient uint64) (int64, error) {
	var count int64
	err := dao.SC.Db.WithContext(ctx).Model(&model.Notification{}).Where("recipient_id = ? AND read_at IS NULL", recipient).Count(&count).Error
	return count, err
}
func ReadNotification(ctx context.Context, recipient, id uint64, all bool) error {
	db := dao.SC.Db.WithContext(ctx)
	if !all {
		var row model.Notification
		if err := db.Select("id").Where("id = ? AND recipient_id = ?", id, recipient).First(&row).Error; err != nil {
			return err
		}
	}
	q := db.Model(&model.Notification{}).Where("recipient_id = ? AND read_at IS NULL", recipient)
	if all {
		q = q.Where("id <= ?", id)
	} else {
		q = q.Where("id = ?", id)
	}
	return q.Update("read_at", time.Now().UTC()).Error
}
