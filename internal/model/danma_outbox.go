package model

import "time"

// RoomMessageSequence stores the last durable sequence allocated in a room.
// The row is locked while a consumer transaction allocates new sequences.
type RoomMessageSequence struct {
	RoomID    uint64    `json:"room_id" gorm:"primaryKey;autoIncrement:false"`
	LastSeq   uint64    `json:"last_seq" gorm:"not null"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DanmaOutbox makes the MySQL write and the intent to publish one transaction.
// Publishing is at-least-once: clients deduplicate by MessageID and order by
// RoomSeq.
type DanmaOutbox struct {
	ID            uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	MessageID     string     `json:"message_id" gorm:"uniqueIndex;size:64;not null"`
	RoomID        uint64     `json:"room_id" gorm:"index:idx_outbox_room_seq,priority:1;not null"`
	RoomSeq       uint64     `json:"room_seq" gorm:"index:idx_outbox_room_seq,priority:2;not null"`
	Payload       []byte     `json:"payload" gorm:"type:json;not null"`
	Attempts      uint       `json:"attempts" gorm:"not null;default:0"`
	NextAttemptAt time.Time  `json:"next_attempt_at" gorm:"index;not null"`
	PublishedAt   *time.Time `json:"published_at" gorm:"index"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
