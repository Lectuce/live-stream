package model

import "time"

type UserFollow struct {
	ID         uint64 `gorm:"primaryKey;autoIncrement"`
	FollowerID uint64 `gorm:"not null;uniqueIndex:idx_user_follow_pair,priority:1"`
	FolloweeID uint64 `gorm:"not null;uniqueIndex:idx_user_follow_pair,priority:2;index"`
	CreatedAt  time.Time
}

// PublicUser deliberately excludes account credentials, email and role.
type PublicUser struct {
	UserID         uint64 `json:"user_id,string"`
	UserName       string `json:"user_name"`
	AvatarURL      string `json:"avatar_url"`
	Bio            string `json:"bio"`
	FollowerCount  int64  `json:"follower_count"`
	FollowingCount int64  `json:"following_count"`
	VideoCount     int64  `json:"video_count"`
}

type FollowState struct {
	Following     bool  `json:"following"`
	FollowerCount int64 `json:"follower_count"`
}
