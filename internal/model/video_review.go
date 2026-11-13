package model

import "time"

type VideoReview struct {
	ReviewID       uint64    `json:"review_id,string" gorm:"primaryKey;autoIncrement"`
	VideoID        uint64    `json:"video_id,string" gorm:"not null;uniqueIndex:idx_video_review_version"`
	ContentVersion uint64    `json:"content_version,string" gorm:"not null;uniqueIndex:idx_video_review_version"`
	ReviewerID     uint64    `json:"reviewer_id,string" gorm:"not null;index"`
	ReviewerType   string    `json:"reviewer_type" gorm:"size:20;not null"`
	Decision       string    `json:"decision" gorm:"size:20;not null"`
	Reason         string    `json:"reason" gorm:"size:1000"`
	CreatedAt      time.Time `json:"created_at"`
}

type ReviewVideoRequest struct {
	ContentVersion uint64 `json:"content_version,string"`
	Decision       string `json:"decision"`
	Reason         string `json:"reason"`
}

type ReviewVideo struct {
	OwnerVideo
	OwnerUsername string `json:"owner_username"`
}
type ReviewVideoPage struct {
	Items      []ReviewVideo `json:"items"`
	NextCursor string        `json:"next_cursor"`
	HasMore    bool          `json:"has_more"`
}
type ReviewVideoDetail struct {
	ReviewVideo
	Reviews []VideoReview `json:"reviews"`
}
