package model

import "time"

// One row per viewer/video, retained as a tombstone after deletion so a delayed
// session-creation request cannot reuse an old version.
type WatchHistory struct {
	ID             uint64     `gorm:"primaryKey;autoIncrement;index:idx_watch_list,priority:6"`
	UserID         uint64     `gorm:"not null;uniqueIndex:uq_watch_user_video,priority:1;index:idx_watch_list,priority:1"`
	VideoID        uint64     `gorm:"not null;uniqueIndex:uq_watch_user_video,priority:2"`
	HistoryEpoch   uint64     `gorm:"not null;index:idx_watch_list,priority:2"`
	PositionMS     uint64     `gorm:"not null"`
	SessionID      string     `gorm:"size:36;not null"`
	SessionVersion uint64     `gorm:"not null"`
	LastSeq        uint64     `gorm:"not null"`
	LastWatchedAt  *time.Time `gorm:"precision:6;index:idx_watch_list,priority:5"`
	DeletedAt      *time.Time `gorm:"precision:6;index:idx_watch_list,priority:3"`
}

func (WatchHistory) TableName() string { return "watch_history" }

// Lock this row before touching a user's history. Clear and progress writes are
// serialized without locking the user's account or scanning all their videos.
type WatchHistoryState struct {
	UserID       uint64 `gorm:"primaryKey;autoIncrement:false"`
	HistoryEpoch uint64 `gorm:"not null"`
}

type WatchProgress struct {
	VideoID        uint64 `json:"video_id,string"`
	PositionMS     uint64 `json:"position_ms"`
	DurationMS     uint64 `json:"duration_ms"`
	HistoryEpoch   uint64 `json:"history_epoch,string"`
	SessionVersion uint64 `json:"session_version,string"`
	SessionID      string `json:"session_id"`
	LastSeq        uint64 `json:"last_seq,string"`
	HasHistory     bool   `json:"has_history"`
}

type WatchSessionRequest struct {
	SessionID      string `json:"session_id"`
	HistoryEpoch   uint64 `json:"history_epoch,string"`
	SessionVersion uint64 `json:"session_version,string"`
}

type WatchProgressRequest struct {
	WatchSessionRequest
	Seq        uint64  `json:"seq"`
	PositionMS *uint64 `json:"position_ms"`
}

type WatchHistoryItem struct {
	ID            uint64    `json:"id,string"`
	VideoID       uint64    `json:"video_id,string"`
	PositionMS    uint64    `json:"position_ms"`
	LastWatchedAt time.Time `json:"last_watched_at"`
	Title         string    `json:"title"`
	CoverURL      string    `json:"cover_url"`
	DurationMS    uint64    `json:"duration_ms"`
	OwnerID       uint64    `json:"owner_id,string"`
	OwnerName     string    `json:"owner_name"`
	Available     bool      `json:"available"`
}
