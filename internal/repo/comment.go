package repo

import (
	"context"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

var ErrCommentTarget = errors.New("invalid comment parent")
var ErrCommentClosed = errors.New("deleted comment cannot receive replies")
var ErrCommentRequest = errors.New("comment request ID reused with different content")

func publicCommentTarget(db *gorm.DB, targetType string, targetID uint64, lock bool) error {
	if lock {
		db = db.Clauses(clause.Locking{Strength: "SHARE"})
	}
	switch targetType {
	case model.CommentTargetVideo:
		var video model.Video
		return db.Select("video_id").Where("video_id = ? AND publish_status = 'published' AND process_status = 'ready' AND audit_status = 'approved'", targetID).First(&video).Error
	case model.CommentTargetPost:
		var post model.Post
		return db.Select("post_id").Where("post_id = ? AND status = 'published'", targetID).First(&post).Error
	default:
		return ErrCommentTarget
	}
}

func matchCommentRequest(comment *model.Comment, targetType string, targetID uint64, req model.CreateCommentRequest) error {
	if comment.DeletedAt != nil {
		return ErrCommentClosed
	}
	if comment.TargetType != targetType || comment.TargetID != targetID || comment.ParentID != req.ParentID || comment.Content != req.Content {
		return ErrCommentRequest
	}
	return nil
}

func existingComment(db *gorm.DB, userID uint64, requestID string) (*model.Comment, error) {
	var c model.Comment
	result := db.Where("user_id = ? AND request_id = ?", userID, requestID).Limit(1).Find(&c)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &c, nil
}

func CreateComment(ctx context.Context, targetType string, targetID, userID uint64, req model.CreateCommentRequest) (*model.Comment, error) {
	var comment model.Comment
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Publishing/unpublishing/deleting takes an exclusive content lock. No new comment commits after a preceding unpublish.
		if err := publicCommentTarget(tx, targetType, targetID, true); err != nil {
			return err
		}
		existing, err := existingComment(tx, userID, req.RequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			comment = *existing
			return matchCommentRequest(existing, targetType, targetID, req)
		}
		comment = model.Comment{TargetType: targetType, TargetID: targetID, UserID: userID, ParentID: req.ParentID, Content: req.Content, RequestID: req.RequestID, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Version: 1, AuditStatus: "pending"}
		if targetType == model.CommentTargetVideo {
			comment.VideoID = targetID
		}
		if req.ParentID != 0 {
			var parent model.Comment
			if err := tx.Where("id = ? AND target_type = ? AND target_id = ?", req.ParentID, targetType, targetID).First(&parent).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrCommentTarget
				}
				return err
			}
			rootID := parent.RootID
			if rootID == 0 {
				rootID = parent.ID
			}
			var root model.Comment
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND target_type = ? AND target_id = ? AND root_id = 0", rootID, targetType, targetID).First(&root).Error; err != nil {
				return err
			}
			if root.DeletedAt != nil || root.AuditStatus != "approved" {
				return ErrCommentClosed
			}
			if parent.ID == root.ID {
				parent = root
			} else {
				// Re-read after locking the root. A concurrent delete must finish before we accept the reply.
				var lockedParent model.Comment
				if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND target_type = ? AND target_id = ? AND root_id = ?", parent.ID, targetType, targetID, rootID).First(&lockedParent).Error; err != nil {
					return err
				}
				parent = lockedParent
			}
			if parent.DeletedAt != nil || parent.AuditStatus != "approved" {
				return ErrCommentClosed
			}
			comment.RootID = rootID
			comment.ReplyToUserID = parent.UserID
		}
		return tx.Create(&comment).Error
	})
	var duplicate *mysql.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		// A simultaneous retry may have committed first. Compare the original command, never create another row.
		existing, lookupErr := existingComment(dao.SC.Db.WithContext(ctx), userID, req.RequestID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if existing != nil {
			return existing, matchCommentRequest(existing, targetType, targetID, req)
		}
	}
	return &comment, err
}

func ListComments(ctx context.Context, targetType string, targetID, rootID uint64, before time.Time, beforeID uint64, limit int) ([]model.CommentView, error) {
	db := dao.SC.Db.WithContext(ctx)
	if err := publicCommentTarget(db, targetType, targetID, false); err != nil {
		return nil, err
	}
	if rootID != 0 {
		var root model.Comment
		if err := db.Select("id").Where("id = ? AND target_type = ? AND target_id = ? AND root_id = 0 AND audit_status = 'approved'", rootID, targetType, targetID).First(&root).Error; err != nil {
			return nil, err
		}
	}
	items := make([]model.CommentView, 0)
	q := publicCommentQuery(db, targetType, targetID).Where("c.root_id = ?", rootID)
	if beforeID > 0 {
		q = q.Where("(c.created_at < ? OR (c.created_at = ? AND c.id < ?))", before, before, beforeID)
	}
	err := q.Order("c.created_at DESC, c.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}

func DeleteComment(ctx context.Context, targetType string, targetID, commentID, userID uint64) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c model.Comment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND target_type = ? AND target_id = ? AND user_id = ?", commentID, targetType, targetID, userID).First(&c).Error; err != nil {
			return err
		}
		if c.DeletedAt != nil {
			return nil
		}
		if err := recordCommentReview(tx, &c, userID, "deleted", ""); err != nil {
			return err
		}
		return tx.Model(&c).Updates(map[string]any{"deleted_at": time.Now().UTC(), "content": "", "version": gorm.Expr("version + 1")}).Error
	})
}

// Existing video callers share the target-scoped implementation.
func CreateVideoComment(ctx context.Context, videoID, userID uint64, req model.CreateCommentRequest) (*model.Comment, error) {
	return CreateComment(ctx, model.CommentTargetVideo, videoID, userID, req)
}
func ListVideoComments(ctx context.Context, videoID, rootID uint64, before time.Time, beforeID uint64, limit int) ([]model.CommentView, error) {
	return ListComments(ctx, model.CommentTargetVideo, videoID, rootID, before, beforeID, limit)
}
func DeleteVideoComment(ctx context.Context, videoID, commentID, userID uint64) error {
	return DeleteComment(ctx, model.CommentTargetVideo, videoID, commentID, userID)
}
