package model

import "time"

type VideoCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var VideoCategories = []VideoCategory{{"general", "综合"}, {"animation", "动画"}, {"games", "游戏"}, {"music", "音乐"}, {"knowledge", "知识"}, {"technology", "科技"}, {"life", "生活"}}

func ValidVideoCategory(value string) bool {
	for _, c := range VideoCategories {
		if c.ID == value {
			return true
		}
	}
	return false
}

// Server-assigned UTC day, viewer and kind form the deduplication key.
type VideoBehavior struct {
	Day     string `gorm:"primaryKey;size:10"`
	UserID  uint64 `gorm:"primaryKey;autoIncrement:false"`
	VideoID uint64 `gorm:"primaryKey;autoIncrement:false"`
	Kind    string `gorm:"primaryKey;size:16"`
}
type VideoMetric struct {
	Day         string `gorm:"primaryKey;size:10"`
	VideoID     uint64 `gorm:"primaryKey;autoIncrement:false"`
	Impressions uint64
	Clicks      uint64
	Plays       uint64
}
type VideoPlaySession struct {
	ID         string `gorm:"primaryKey;size:36"`
	UserID     uint64 `gorm:"not null"`
	VideoID    uint64 `gorm:"not null"`
	LastSeq    uint64
	PositionMS uint64
	WatchedMS  uint64
	UpdatedAt  time.Time `gorm:"precision:6"`
	ExpiresAt  time.Time `gorm:"precision:6;index"`
}
type VideoCard struct {
	VideoID     uint64    `json:"video_id,string"`
	OwnerID     uint64    `json:"owner_id,string"`
	OwnerName   string    `json:"owner_name"`
	Title       string    `json:"title"`
	CoverURL    string    `json:"cover_url"`
	Category    string    `json:"category"`
	DurationMS  uint64    `json:"duration_ms"`
	PublishedAt time.Time `json:"published_at"`
}
type VideoDiscoveryPage struct {
	Items         []VideoCard `json:"items"`
	NextCursor    string      `json:"next_cursor"`
	HasMore       bool        `json:"has_more"`
	Mode          string      `json:"mode"`
	Degraded      bool        `json:"degraded"`
	DeliveryToken string      `json:"delivery_token"`
}
type VideoSignals struct {
	VideoID                                   uint64
	Plays, Clicks, Likes, Favorites, Comments float64
}
type SearchItem struct {
	ID    uint64 `json:"id,string"`
	Title string `json:"title"`
	Image string `json:"image"`
	Kind  string `json:"kind"`
}
