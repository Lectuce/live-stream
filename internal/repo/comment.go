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

func publicCommentVideo(db *gorm.DB, videoID uint64, lock bool) error {
	var video model.Video
	query := db.Select("video_id").Where("video_id = ? AND publish_status = 'published' AND process_status = 'ready' AND audit_status = 'approved'", videoID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "SHARE"})
	}
	return query.First(&video).Error
}

func matchCommentRequest(comment *model.Comment, videoID uint64, req model.CreateCommentRequest) error {
	if comment.DeletedAt != nil {
		return ErrCommentClosed
	}
	if comment.VideoID != videoID || comment.ParentID != req.ParentID || comment.Content != req.Content {
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

func CreateVideoComment(ctx context.Context, videoID, userID uint64, req model.CreateCommentRequest) (*model.Comment, error) {
	var comment model.Comment
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Publishing/unpublishing takes an exclusive video lock. No new comment commits after a preceding unpublish.
		if err := publicCommentVideo(tx, videoID, true); err != nil {
			return err
		}
		existing, err := existingComment(tx, userID, req.RequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			comment = *existing
			return matchCommentRequest(existing, videoID, req)
		}
		comment = model.Comment{VideoID: videoID, UserID: userID, ParentID: req.ParentID, Content: req.Content, RequestID: req.RequestID, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if req.ParentID != 0 {
			var parent model.Comment
			if err := tx.Where("id = ? AND video_id = ?", req.ParentID, videoID).First(&parent).Error; err != nil {
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
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND video_id = ? AND root_id = 0", rootID, videoID).First(&root).Error; err != nil {
				return err
			}
			if root.DeletedAt != nil {
				return ErrCommentClosed
			}
			if parent.ID == root.ID {
				parent = root
			} else {
				// Re-read after locking the root. A concurrent delete must finish before we accept the reply.
				var lockedParent model.Comment
				if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND video_id = ? AND root_id = ?", parent.ID, videoID, rootID).First(&lockedParent).Error; err != nil {
					return err
				}
				parent = lockedParent
			}
			if parent.DeletedAt != nil {
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
			return existing, matchCommentRequest(existing, videoID, req)
		}
	}
	return &comment, err
}

func ListVideoComments(ctx context.Context, videoID, rootID uint64, before time.Time, beforeID uint64, limit int) ([]model.CommentView, error) {
	db := dao.SC.Db.WithContext(ctx)
	if err := publicCommentVideo(db, videoID, false); err != nil {
		return nil, err
	}
	if rootID != 0 {
		var root model.Comment
		if err := db.Select("id").Where("id = ? AND video_id = ? AND root_id = 0", rootID, videoID).First(&root).Error; err != nil {
			return nil, err
		}
	}
	items := make([]model.CommentView, 0)
	q := db.Table("comments AS c").Select(`c.id,c.video_id,c.user_id,c.root_id,c.parent_id,c.reply_to_user_id,
		CASE WHEN c.deleted_at IS NULL THEN c.content ELSE '' END AS content,c.created_at,c.deleted_at,
		COALESCE(u.user_name,'已注销用户') AS user_name,COALESCE(u.avatar_url,'') AS avatar_url,
		COALESCE(ru.user_name,'') AS reply_to_user_name,
		(SELECT COUNT(*) FROM comments AS r WHERE r.video_id = c.video_id AND r.root_id = c.id) AS reply_count`).
		Joins("JOIN videos AS v ON v.video_id = c.video_id AND v.publish_status = 'published' AND v.process_status = 'ready' AND v.audit_status = 'approved' AND v.deleted_at IS NULL").
		Joins("LEFT JOIN users AS u ON u.user_id = c.user_id").Joins("LEFT JOIN users AS ru ON ru.user_id = c.reply_to_user_id").
		Where("c.video_id = ? AND c.root_id = ?", videoID, rootID)
	if beforeID > 0 {
		q = q.Where("(c.created_at < ? OR (c.created_at = ? AND c.id < ?))", before, before, beforeID)
	}
	err := q.Order("c.created_at DESC, c.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}

func DeleteVideoComment(ctx context.Context, videoID, commentID, userID uint64) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c model.Comment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND video_id = ? AND user_id = ?", commentID, videoID, userID).First(&c).Error; err != nil {
			return err
		}
		if c.DeletedAt != nil {
			return nil
		}
		return tx.Model(&c).Updates(map[string]any{"deleted_at": time.Now().UTC(), "content": ""}).Error
	})
}
