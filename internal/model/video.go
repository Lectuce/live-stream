package model

import "time"

type Video struct {
	VideoID uint64 `json:"video_id,string" gorm:"primaryKey;autoIncrement"`
	OwnerID uint64 `json:"owner_id,string" gorm:"not null;index"`

	Title       string `json:"title" gorm:"size:100;not null"`
	Description string `json:"description" gorm:"type:text"`
	CoverURL    string `json:"cover_url" gorm:"size:500"`
	PlayURL     string `json:"play_url" gorm:"size:500"`
	MediaFile   string `json:"-" gorm:"size:100"`

	DurationMS uint64 `json:"duration_ms"`

	ProcessStatus string `json:"process_status" gorm:"size:20;not null;default:pending"`
	PublishStatus string `json:"publish_status" gorm:"size:20;not null;default:draft"`
	AuditStatus   string `json:"audit_status" gorm:"size:20;not null;default:pending"`

	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateVideoRequest struct {
	Title       string `json:"title" binding:"required,max=100"`
	Description string `json:"description" binding:"max=5000"`
}

type VideoPage struct {
	Items      []Video `json:"items"`
	NextCursor string  `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}
