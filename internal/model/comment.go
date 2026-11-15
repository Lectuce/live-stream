package model

import "time"

const (
	CommentTargetVideo = "video"
	CommentTargetPost  = "post"
)

// Deleted comments remain as tombstones to preserve the reply tree.
type Comment struct {
	ID uint64 `json:"comment_id,string" gorm:"primaryKey;autoIncrement;index:idx_comment_target_page,priority:5;index:idx_comment_hot,priority:7"`
	// VideoID is retained for migration and compatibility with existing video clients.
	VideoID       uint64     `json:"video_id,string,omitempty" gorm:"not null;default:0"`
	TargetType    string     `json:"target_type" gorm:"size:16;not null;default:video;index:idx_comment_target_page,priority:1;index:idx_comment_hot,priority:1"`
	TargetID      uint64     `json:"target_id,string" gorm:"not null;default:0;index:idx_comment_target_page,priority:2;index:idx_comment_hot,priority:2"`
	UserID        uint64     `json:"user_id,string" gorm:"not null;uniqueIndex:idx_comment_request,priority:1"`
	RootID        uint64     `json:"root_id,string" gorm:"not null;default:0;index:idx_comment_target_page,priority:3;index:idx_comment_hot,priority:3"`
	ParentID      uint64     `json:"parent_id,string" gorm:"not null;default:0"`
	ReplyToUserID uint64     `json:"reply_to_user_id,string" gorm:"not null;default:0"`
	RequestID     string     `json:"-" gorm:"size:36;not null;uniqueIndex:idx_comment_request,priority:2"`
	Content       string     `json:"content" gorm:"type:text;not null"`
	CreatedAt     time.Time  `json:"created_at" gorm:"type:datetime(6);not null;index:idx_comment_target_page,priority:4"`
	DeletedAt     *time.Time `json:"deleted_at" gorm:"type:datetime(6);index:idx_comment_hot,priority:5"`
	Version       uint64     `json:"version,string" gorm:"not null;default:1"`
	AuditStatus   string     `json:"audit_status" gorm:"size:16;not null;default:approved;index:idx_comment_review,priority:1;index:idx_comment_hot,priority:4"`
	AuditReason   string     `json:"audit_reason" gorm:"size:1000;not null;default:''"`
	LikeCount     int64      `json:"like_count" gorm:"not null;default:0;index:idx_comment_hot,priority:6"`
}

type CommentView struct {
	Comment
	UserName        string `json:"user_name"`
	AvatarURL       string `json:"avatar_url"`
	ReplyToUserName string `json:"reply_to_user_name"`
	ReplyCount      int64  `json:"reply_count"`
	Liked           bool   `json:"liked" gorm:"-"`
}

type CommentLike struct {
	CommentID uint64 `gorm:"primaryKey;autoIncrement:false"`
	UserID    uint64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
}

// Store hashes rather than archived bodies: deleting a comment removes its text.
type CommentReview struct {
	ID          uint64    `json:"review_id,string" gorm:"primaryKey"`
	CommentID   uint64    `json:"comment_id,string" gorm:"not null;index"`
	ActorID     uint64    `json:"actor_id,string" gorm:"not null"`
	Version     uint64    `json:"version,string" gorm:"not null"`
	Action      string    `json:"action" gorm:"size:16;not null"`
	Reason      string    `json:"reason" gorm:"size:1000;not null"`
	ContentHash string    `json:"content_hash" gorm:"size:64;not null"`
	CreatedAt   time.Time `json:"created_at"`
}

type EditCommentRequest struct {
	Version uint64 `json:"version,string"`
	Content string `json:"content"`
}
type ReviewCommentRequest struct {
	Version  uint64 `json:"version,string"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}
type CommentReport struct {
	ID         uint64     `json:"report_id,string" gorm:"primaryKey"`
	CommentID  uint64     `json:"comment_id,string" gorm:"not null;uniqueIndex:idx_comment_report,priority:1"`
	UserID     uint64     `json:"user_id,string" gorm:"not null;uniqueIndex:idx_comment_report,priority:2"`
	Reason     string     `json:"reason" gorm:"size:1000;not null"`
	Status     string     `json:"status" gorm:"size:16;not null;default:pending;index"`
	Resolution string     `json:"resolution" gorm:"size:1000;not null;default:''"`
	ResolvedBy uint64     `json:"resolved_by,string" gorm:"not null;default:0"`
	ResolvedAt *time.Time `json:"resolved_at"`
	CreatedAt  time.Time  `json:"created_at"`
}
type CommentReportView struct {
	CommentReport
	Content        string `json:"content"`
	CommentVersion uint64 `json:"comment_version,string"`
	AuditStatus    string `json:"audit_status"`
}

type CommentPage struct {
	Items      []CommentView `json:"items"`
	NextCursor string        `json:"next_cursor"`
	HasMore    bool          `json:"has_more"`
	Truncated  bool          `json:"truncated"`
}

type CreateCommentRequest struct {
	Content   string `json:"content"`
	ParentID  uint64 `json:"parent_id,string"`
	RequestID string `json:"request_id"`
}
