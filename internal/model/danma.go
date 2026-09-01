package model

import "time"

type Danma struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	MessageID string    `json:"message_id" gorm:"uniqueIndex:idx_msg_id;size:64;not null"`
	RoomID    uint64    `json:"room_id" gorm:"index:idx_room_history,priority:1;not null"`
	RoomSeq   uint64    `json:"room_seq" gorm:"column:room_seq;not null"`
	UserID    uint64    `json:"user_id" gorm:"index;not null"`
	UserName  string    `json:"user_name" gorm:"size:64;not null"`
	Content   string    `json:"content" gorm:"type:varchar(500);not null"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_room_history,priority:2"`
}
