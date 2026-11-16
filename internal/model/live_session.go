package model

import "time"

// A stream name is never reused, including after encoder reconnects.
type LiveSession struct {
	ID        uint64     `json:"session_id,string" gorm:"primaryKey"`
	RoomID    uint64     `json:"room_id" gorm:"index;not null"`
	OwnerID   uint64     `json:"-"`
	Stream    string     `json:"-" gorm:"size:36;uniqueIndex"`
	Title     string     `json:"title" gorm:"size:400"`
	Status    string     `json:"status" gorm:"size:16;index"`
	ClientID  string     `json:"-" gorm:"size:100"`
	ExpiresAt time.Time  `json:"expires_at"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"-"`
}

type FeedLive struct {
	SessionID uint64 `json:"session_id,string"`
	RoomID    uint64 `json:"room_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
}
