package model

import "time"

type RoomDetail struct {
	RoomID    uint64    `json:"room_id" gorm:"primaryKey;autoIncrement"`
	RoomTitle string    `json:"room_title" mapstructure:"room_title"`
	Status    int       `json:"status" mapstructure:"status"`
	OwnerID   int64     `json:"owner_id" mapstructure:"owner_id"`
	StartAt   time.Time `json:"strated_at" mapstructure:"strated_at"`
	LikeCount int64     `json:"like_count" gorm:"column:like_count"`
	PlayURL   string    `json:"play_url" mapstructure:"play_url"` // hls播放地址，详情接口返回
}
