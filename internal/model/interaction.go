package model

import "time"

type ContentLike struct {
	TargetType string `gorm:"primaryKey;size:16"`
	TargetID   uint64 `gorm:"primaryKey;autoIncrement:false"`
	UserID     uint64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt  time.Time
}
type ContentStat struct {
	TargetType string `gorm:"primaryKey;size:16"`
	TargetID   uint64 `gorm:"primaryKey;autoIncrement:false"`
	LikeCount  int64  `gorm:"not null;default:0"`
}
type ContentLikeState struct {
	Liked     bool  `json:"liked"`
	LikeCount int64 `json:"like_count"`
}
type FavoriteFolder struct {
	ID        uint64    `json:"folder_id,string" gorm:"primaryKey"`
	OwnerID   uint64    `json:"-" gorm:"not null;uniqueIndex:idx_folder_slot,priority:1"`
	Slot      string    `json:"-" gorm:"size:36;not null;uniqueIndex:idx_folder_slot,priority:2"`
	Name      string    `json:"name" gorm:"size:60;not null"`
	IsDefault bool      `json:"is_default" gorm:"not null;default:false"`
	CreatedAt time.Time `json:"created_at"`
}
type FavoriteItem struct {
	ID        uint64    `json:"item_id,string" gorm:"primaryKey"`
	FolderID  uint64    `json:"folder_id,string" gorm:"not null;uniqueIndex:idx_favorite_pair,priority:1"`
	VideoID   uint64    `json:"video_id,string" gorm:"not null;uniqueIndex:idx_favorite_pair,priority:2"`
	CreatedAt time.Time `json:"created_at"`
}
type FavoriteView struct {
	FavoriteItem
	Title     string `json:"title"`
	CoverURL  string `json:"cover_url"`
	Available bool   `json:"available"`
}
type NotificationData struct {
	EventKey    string    `json:"-" gorm:"size:120;not null;uniqueIndex"`
	RecipientID uint64    `json:"-" gorm:"not null;index:idx_notification_recipient,priority:1"`
	ActorID     uint64    `json:"actor_id,string" gorm:"not null"`
	Kind        string    `json:"kind" gorm:"size:32;not null"`
	TargetType  string    `json:"target_type" gorm:"size:16;not null"`
	TargetID    uint64    `json:"target_id,string" gorm:"not null"`
	CreatedAt   time.Time `json:"created_at"`
}
type NotificationOutbox struct {
	ID uint64 `gorm:"primaryKey"`
	NotificationData
	DeliveredAt *time.Time `gorm:"index"`
}
type Notification struct {
	ID uint64 `json:"notification_id,string" gorm:"primaryKey"`
	NotificationData
	ReadAt *time.Time `json:"read_at" gorm:"index:idx_notification_recipient,priority:2"`
}
type NotificationView struct {
	Notification
	ActorName string `json:"actor_name"`
	URL       string `json:"url"`
}
type Report struct {
	ID         uint64     `json:"report_id,string" gorm:"primaryKey"`
	TargetType string     `json:"target_type" gorm:"size:16;not null;uniqueIndex:idx_report_target_user,priority:1"`
	TargetID   string     `json:"target_id" gorm:"size:64;not null;uniqueIndex:idx_report_target_user,priority:2"`
	UserID     uint64     `json:"user_id,string" gorm:"not null;uniqueIndex:idx_report_target_user,priority:3"`
	Reason     string     `json:"reason" gorm:"size:1000;not null"`
	Status     string     `json:"status" gorm:"size:16;not null;default:pending;index"`
	Resolution string     `json:"resolution" gorm:"size:1000;not null;default:''"`
	ResolvedBy uint64     `json:"resolved_by,string" gorm:"not null;default:0"`
	ResolvedAt *time.Time `json:"resolved_at"`
	CreatedAt  time.Time  `json:"created_at"`
}
type ReportAction struct {
	ID        uint64    `json:"action_id,string" gorm:"primaryKey"`
	ReportID  uint64    `json:"report_id,string" gorm:"not null;index"`
	ActorID   uint64    `json:"actor_id,string" gorm:"not null"`
	Action    string    `json:"action" gorm:"size:16;not null"`
	Reason    string    `json:"reason" gorm:"size:1000;not null"`
	CreatedAt time.Time `json:"created_at"`
}
