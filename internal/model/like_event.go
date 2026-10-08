package model

import "time"

// LikeEvent 仅用于描述旧版逐次点赞表。新的写入使用 LikeBatch 和
// LikeAggregationCheckpoint，使 MySQL 不再为每次点赞都保存一行记录并更新
// 房间热点行。
type LikeEvent struct {
	MessageID string    `json:"message_id" gorm:"primaryKey;size:64"`
	RoomID    uint64    `json:"room_id" gorm:"index;not null"`
	UserID    uint64    `json:"user_id" gorm:"index;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"not null"`
}
