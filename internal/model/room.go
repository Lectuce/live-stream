package model

import "time"

type Room struct {
	RoomID    uint64     `json:"room_id" gorm:"primaryKey;autoIncrement"`
	RoomTitle string     `json:"room_title" mapstructure:"room_title"`
	Status    int        `json:"status" mapstructure:"status" gorm:"index:idx_rooms_status_start,priority:1"`
	OwnerID   uint64     `json:"owner_id" mapstructure:"owner_id"`
	CreatedAt time.Time  `json:"created_at" mapstructure:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" mapstructure:"updated_at"`
	StartAt   *time.Time `json:"start_at" mapstructure:"start_at" gorm:"column:start_at;index:idx_rooms_status_start,priority:2"`
	LikeCount int64      `json:"like_count" gorm:"column:like_count"`
	// 直播相关地址
	PushURL string `json:"-" mapstructure:"push_url"`        // 推流地址，json:"-" 不序列化返回前端（敏感）
	PlayURL string `json:"play_url" mapstructure:"play_url"` // hls播放地址，详情接口返回
}
