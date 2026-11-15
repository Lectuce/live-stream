package repo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

var ErrCommentConflict = errors.New("comment version changed")

func publicCommentQuery(db *gorm.DB, kind string, id uint64) *gorm.DB {
	q := db.Table("comments AS c").Select(`c.id,c.video_id,c.target_type,c.target_id,c.user_id,c.root_id,c.parent_id,c.reply_to_user_id,
		CASE WHEN c.deleted_at IS NULL THEN c.content ELSE '' END AS content,c.created_at,c.deleted_at,c.version,c.audit_status,c.like_count,
		COALESCE(u.user_name,'已注销用户') AS user_name,COALESCE(u.avatar_url,'') AS avatar_url,
		COALESCE(ru.user_name,'') AS reply_to_user_name,
		(SELECT COUNT(*) FROM comments AS r WHERE r.target_type = c.target_type AND r.target_id = c.target_id AND r.root_id = c.id AND r.audit_status = 'approved') AS reply_count`).
		Joins("LEFT JOIN users AS u ON u.user_id = c.user_id").Joins("LEFT JOIN users AS ru ON ru.user_id = c.reply_to_user_id").
		Where("c.target_type = ? AND c.target_id = ?", kind, id).
		Where("c.audit_status = 'approved'").
		Where("(c.root_id = 0 OR EXISTS (SELECT 1 FROM comments AS root WHERE root.id = c.root_id AND root.target_type = c.target_type AND root.target_id = c.target_id AND root.audit_status = 'approved'))")
	if kind == model.CommentTargetVideo {
		q = q.Joins("JOIN videos AS v ON v.video_id = c.target_id AND v.publish_status = 'published' AND v.process_status = 'ready' AND v.audit_status = 'approved' AND v.deleted_at IS NULL")
	} else {
		q = q.Joins("JOIN posts AS p ON p.post_id = c.target_id AND p.status = 'published' AND p.deleted_at IS NULL")
	}
	return q
}

// All interactions lock the root before the child, matching reply creation.
func lockComment(tx *gorm.DB, id uint64, public bool) (*model.Comment, error) {
	var original model.Comment
	if err := tx.First(&original, id).Error; err != nil {
		return nil, err
	}
	if public {
		if err := publicCommentTarget(tx, original.TargetType, original.TargetID, true); err != nil {
			return nil, err
		}
	}
	if original.RootID != 0 {
		var root model.Comment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND target_type = ? AND target_id = ? AND root_id = 0", original.RootID, original.TargetType, original.TargetID).First(&root).Error; err != nil {
			return nil, err
		}
		if public && root.AuditStatus != "approved" {
			return nil, ErrCommentClosed
		}
	}
	var current model.Comment
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
		return nil, err
	}
	if current.DeletedAt != nil {
		return nil, ErrCommentClosed
	}
	return &current, nil
}

func recordCommentReview(tx *gorm.DB, c *model.Comment, actor uint64, action, reason string) error {
	hash := sha256.Sum256([]byte(c.Content))
	return tx.Create(&model.CommentReview{CommentID: c.ID, ActorID: actor, Version: c.Version, Action: action, Reason: reason, ContentHash: fmt.Sprintf("%x", hash)}).Error
}

func EditComment(ctx context.Context, id, actor uint64, req model.EditCommentRequest) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, err := lockComment(tx, id, false)
		if err != nil {
			return err
		}
		if c.UserID != actor {
			return gorm.ErrRecordNotFound
		}
		if c.Version != req.Version {
			return ErrCommentConflict
		}
		// Editing never republishes a hidden comment; every changed body returns to review.
		if err := recordCommentReview(tx, c, actor, "edited", ""); err != nil {
			return err
		}
		return tx.Model(c).Updates(map[string]any{"content": req.Content, "audit_status": "pending", "audit_reason": "", "version": gorm.Expr("version + 1")}).Error
	})
}

func ReviewComment(ctx context.Context, id, actor uint64, req model.ReviewCommentRequest) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, err := lockComment(tx, id, false)
		if err != nil {
			return err
		}
		if c.Version != req.Version {
			return ErrCommentConflict
		}
		if err := recordCommentReview(tx, c, actor, req.Decision, req.Reason); err != nil {
			return err
		}
		return tx.Model(c).Updates(map[string]any{"audit_status": req.Decision, "audit_reason": req.Reason, "version": gorm.Expr("version + 1")}).Error
	})
}

func SetCommentLike(ctx context.Context, id, userID uint64, liked bool) (int64, error) {
	var count int64
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, err := lockComment(tx, id, true)
		if err != nil {
			return err
		}
		if c.AuditStatus != "approved" {
			return gorm.ErrRecordNotFound
		}
		var exists int64
		if err := tx.Model(&model.CommentLike{}).Where("comment_id = ? AND user_id = ?", id, userID).Count(&exists).Error; err != nil {
			return err
		}
		count = c.LikeCount
		if liked && exists == 0 {
			if err := tx.Create(&model.CommentLike{CommentID: id, UserID: userID}).Error; err != nil {
				return err
			}
			count++
		} else if !liked && exists != 0 {
			if err := tx.Where("comment_id = ? AND user_id = ?", id, userID).Delete(&model.CommentLike{}).Error; err != nil {
				return err
			}
			count = max(0, count-1)
		} else {
			return nil
		}
		// The comment lock serializes relationship and counter changes. No full COUNT on the hot write path.
		return tx.Model(c).Update("like_count", count).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return count, err
}

func FillCommentLikes(ctx context.Context, items []model.CommentView, viewer uint64) error {
	if viewer == 0 || len(items) == 0 {
		return nil
	}
	ids := make([]uint64, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	var likes []model.CommentLike
	if err := dao.SC.Db.WithContext(ctx).Where("user_id = ? AND comment_id IN ?", viewer, ids).Find(&likes).Error; err != nil {
		return err
	}
	set := map[uint64]bool{}
	for _, like := range likes {
		set[like.CommentID] = true
	}
	for i := range items {
		items[i].Liked = set[items[i].ID]
	}
	return nil
}

func ListOwnComments(ctx context.Context, viewer uint64, kind string, targetID, before uint64, limit int) ([]model.CommentView, error) {
	items := make([]model.CommentView, 0)
	q := dao.SC.Db.WithContext(ctx).Table("comments AS c").Select("c.*, COALESCE(u.user_name,'') AS user_name, COALESCE(u.avatar_url,'') AS avatar_url").Joins("LEFT JOIN users AS u ON u.user_id = c.user_id").Where("c.user_id = ? AND c.target_type = ? AND c.target_id = ? AND c.deleted_at IS NULL", viewer, kind, targetID)
	if before != 0 {
		q = q.Where("c.id < ?", before)
	}
	err := q.Order("c.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}

func ListReviewComments(ctx context.Context, status string, before uint64, limit int) ([]model.CommentView, error) {
	items := make([]model.CommentView, 0)
	q := dao.SC.Db.WithContext(ctx).Table("comments AS c").Select("c.*, COALESCE(u.user_name,'') AS user_name").Joins("LEFT JOIN users AS u ON u.user_id = c.user_id").Where("c.audit_status = ? AND c.deleted_at IS NULL", status)
	if before > 0 {
		q = q.Where("c.id < ?", before)
	}
	err := q.Order("c.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}

func CommentHistory(ctx context.Context, id, before uint64, limit int) ([]model.CommentReview, error) {
	items := make([]model.CommentReview, 0)
	q := dao.SC.Db.WithContext(ctx).Where("comment_id = ?", id)
	if before > 0 {
		q = q.Where("id < ?", before)
	}
	err := q.Order("id DESC").Limit(limit).Find(&items).Error
	return items, err
}

func ReportComment(ctx context.Context, id, actor uint64, reason string) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, err := lockComment(tx, id, true)
		if err != nil {
			return err
		}
		if c.AuditStatus != "approved" {
			return gorm.ErrRecordNotFound
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CommentReport{CommentID: id, UserID: actor, Reason: reason, Status: "pending"}).Error
	})
}

func ListCommentReports(ctx context.Context, status string, before uint64, limit int) ([]model.CommentReportView, error) {
	items := make([]model.CommentReportView, 0)
	q := dao.SC.Db.WithContext(ctx).Table("comment_reports AS r").Select("r.*, CASE WHEN c.deleted_at IS NULL THEN c.content ELSE '' END AS content, c.version AS comment_version, c.audit_status").Joins("JOIN comments AS c ON c.id = r.comment_id").Where("r.status = ?", status)
	if before > 0 {
		q = q.Where("r.id < ?", before)
	}
	err := q.Order("r.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}

func ResolveCommentReport(ctx context.Context, id, actor uint64, status, reason string) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var report model.CommentReport
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&report, id).Error; err != nil {
			return err
		}
		if report.Status != "pending" {
			if report.Status == status && report.Resolution == reason && report.ResolvedBy == actor {
				return nil
			}
			return ErrCommentConflict
		}
		return tx.Model(&report).Updates(map[string]any{"status": status, "resolution": reason, "resolved_by": actor, "resolved_at": time.Now().UTC()}).Error
	})
}

// Bounded maintenance batch; run repeatedly using the last returned ID to rebuild ranking counters.
func RebuildCommentLikes(ctx context.Context, after uint64, limit int) (uint64, error) {
	if limit < 1 || limit > 500 {
		return after, ErrCommentTarget
	}
	var ids []uint64
	if err := dao.SC.Db.WithContext(ctx).Model(&model.Comment{}).Where("id > ?", after).Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return after, err
	}
	for _, id := range ids {
		if err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var c model.Comment
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, id).Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&model.CommentLike{}).Where("comment_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
			return tx.Model(&c).Update("like_count", count).Error
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
			return after, err
		}
		after = id
	}
	return after, nil
}

func RankedCommentIDs(ctx context.Context, kind string, targetID uint64, limit int) ([]uint64, error) {
	db := dao.SC.Db.WithContext(ctx)
	if err := publicCommentTarget(db, kind, targetID, false); err != nil {
		return nil, err
	}
	ids := make([]uint64, 0)
	err := publicCommentQuery(db, kind, targetID).Select("c.id").Where("c.root_id = 0 AND c.deleted_at IS NULL").Order("c.like_count DESC, c.id DESC").Limit(limit).Scan(&ids).Error
	return ids, err
}

func CommentsByIDs(ctx context.Context, kind string, targetID uint64, ids []uint64) ([]model.CommentView, error) {
	items := make([]model.CommentView, 0)
	if len(ids) == 0 {
		return items, nil
	}
	db := dao.SC.Db.WithContext(ctx)
	if err := publicCommentTarget(db, kind, targetID, false); err != nil {
		return nil, err
	}
	err := publicCommentQuery(db, kind, targetID).Where("c.root_id = 0 AND c.deleted_at IS NULL AND c.id IN ?", ids).Scan(&items).Error
	return items, err
}
