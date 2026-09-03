package model

import "time"

// LikeEvent is retained only to describe the legacy per-click table. New
// writes use LikeBatch plus LikeAggregationCheckpoint so MySQL no longer
// stores one row and updates the room hot row for every click.
type LikeEvent struct {
	MessageID string    `json:"message_id" gorm:"primaryKey;size:64"`
	RoomID    uint64    `json:"room_id" gorm:"index;not null"`
	UserID    uint64    `json:"user_id" gorm:"index;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"not null"`
}
