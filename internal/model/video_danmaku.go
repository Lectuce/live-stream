package model

import "time"

type VideoDanmaku struct {
	ID         uint64     `json:"danmaku_id,string" gorm:"primaryKey;index:idx_vdm_window,priority:3"`
	VideoID    uint64     `json:"video_id,string" gorm:"not null;index:idx_vdm_window,priority:1"`
	UserID     uint64     `json:"user_id,string" gorm:"not null;uniqueIndex:idx_vdm_request,priority:1"`
	RequestID  string     `json:"-" gorm:"size:36;not null;uniqueIndex:idx_vdm_request,priority:2"`
	PositionMS uint64     `json:"position_ms" gorm:"not null;index:idx_vdm_window,priority:2"`
	Content    string     `json:"content" gorm:"size:100;not null"`
	Color      string     `json:"color" gorm:"size:7;not null"`
	Version    uint64     `json:"version,string" gorm:"not null;default:1"`
	Hidden     bool       `json:"-" gorm:"not null;default:false"`
	DeletedAt  *time.Time `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
}
