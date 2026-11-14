package model

import "time"

// Deleted comments remain as tombstones to preserve the reply tree.
type Comment struct {
	ID            uint64     `json:"comment_id,string" gorm:"primaryKey;autoIncrement;index:idx_comment_page,priority:4"`
	VideoID       uint64     `json:"video_id,string" gorm:"not null;index:idx_comment_page,priority:1"`
	UserID        uint64     `json:"user_id,string" gorm:"not null;uniqueIndex:idx_comment_request,priority:1"`
	RootID        uint64     `json:"root_id,string" gorm:"not null;default:0;index:idx_comment_page,priority:2"`
	ParentID      uint64     `json:"parent_id,string" gorm:"not null;default:0"`
	ReplyToUserID uint64     `json:"reply_to_user_id,string" gorm:"not null;default:0"`
	RequestID     string     `json:"-" gorm:"size:36;not null;uniqueIndex:idx_comment_request,priority:2"`
	Content       string     `json:"content" gorm:"type:text;not null"`
	CreatedAt     time.Time  `json:"created_at" gorm:"type:datetime(6);not null;index:idx_comment_page,priority:3"`
	DeletedAt     *time.Time `json:"deleted_at" gorm:"type:datetime(6)"`
}

type CommentView struct {
	Comment
	UserName        string `json:"user_name"`
	AvatarURL       string `json:"avatar_url"`
	ReplyToUserName string `json:"reply_to_user_name"`
	ReplyCount      int64  `json:"reply_count"`
}

type CommentPage struct {
	Items      []CommentView `json:"items"`
	NextCursor string        `json:"next_cursor"`
	HasMore    bool          `json:"has_more"`
}

type CreateCommentRequest struct {
	Content   string `json:"content"`
	ParentID  uint64 `json:"parent_id,string"`
	RequestID string `json:"request_id"`
}
