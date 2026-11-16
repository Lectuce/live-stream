package model

import (
	"gorm.io/gorm"
	"time"
)

type Video struct {
	VideoID  uint64 `json:"video_id,string" gorm:"primaryKey;autoIncrement;index:idx_video_public_time,priority:6;index:idx_video_public_id,priority:5"`
	OwnerID  uint64 `json:"owner_id,string" gorm:"not null;index"`
	Category string `json:"category" gorm:"size:24;not null;default:general;index:idx_video_category,priority:1"`

	Title       string `json:"title" gorm:"size:100;not null"`
	Description string `json:"description" gorm:"type:text"`
	CoverURL    string `json:"cover_url" gorm:"size:500"`
	PlayURL     string `json:"play_url" gorm:"size:500"`
	MediaFile   string `json:"-" gorm:"size:100"`

	DurationMS uint64 `json:"duration_ms"`

	ProcessStatus  string     `json:"process_status" gorm:"size:20;not null;default:pending;index:idx_video_public_time,priority:2;index:idx_video_public_id,priority:2"`
	PublishStatus  string     `json:"publish_status" gorm:"size:20;not null;default:draft;index:idx_video_public_time,priority:1;index:idx_video_public_id,priority:1"`
	AuditStatus    string     `json:"audit_status" gorm:"size:20;not null;default:pending;index:idx_video_public_time,priority:3;index:idx_video_public_id,priority:3"`
	ContentVersion uint64     `json:"content_version,string" gorm:"not null;default:1"`
	AuditReason    string     `json:"-" gorm:"size:1000"`
	SubmittedAt    *time.Time `json:"submitted_at"`

	PublishedAt *time.Time     `json:"published_at" gorm:"index:idx_video_category,priority:2;index:idx_video_published;index:idx_video_public_time,priority:5"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index;index:idx_video_public_time,priority:4;index:idx_video_public_id,priority:4"`
}

type UpdateVideoRequest struct {
	Category    *string `json:"category"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

type CreateVideoRequest struct {
	Category    string `json:"category"`
	Title       string `json:"title" binding:"required,max=100"`
	Description string `json:"description" binding:"max=5000"`
}

type VideoPage struct {
	Items      []Video `json:"items"`
	NextCursor string  `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// OwnerVideo exposes upload presence without disclosing the storage filename.
type OwnerVideo struct {
	Video
	HasMedia    bool   `json:"has_media"`
	AuditReason string `json:"audit_reason"`
}

func ToOwnerVideo(v Video) OwnerVideo {
	return OwnerVideo{Video: v, HasMedia: v.MediaFile != "", AuditReason: v.AuditReason}
}

type OwnerVideoPage struct {
	Items      []OwnerVideo `json:"items"`
	NextCursor string       `json:"next_cursor"`
	HasMore    bool         `json:"has_more"`
}
