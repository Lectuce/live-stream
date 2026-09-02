package model

import "time"

// LikeEvent is the durable idempotency record for a logical like operation.
// Users may like a room repeatedly, but one message_id can increase the room
// counter only once, including after Kafka replay or a Redis restart.
type LikeEvent struct {
	MessageID string    `json:"message_id" gorm:"primaryKey;size:64"`
	RoomID    uint64    `json:"room_id" gorm:"index;not null"`
	UserID    uint64    `json:"user_id" gorm:"index;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"not null"`
}
