package model

import "time"

// ContentEvent is immutable. The same ID and business key survive every retry.
type ContentEvent struct {
	EventID     string    `json:"event_id" gorm:"size:36;not null;uniqueIndex"`
	BusinessKey string    `json:"business_key" gorm:"size:100;not null;uniqueIndex"`
	ActorID     uint64    `json:"actor_id,string" gorm:"not null;index:idx_content_actor_time,priority:1"`
	EventType   string    `json:"event_type" gorm:"size:40;not null"`
	TargetID    uint64    `json:"target_id,string" gorm:"not null"`
	PublishedAt time.Time `json:"published_at" gorm:"type:datetime(6);not null;index;index:idx_content_actor_time,priority:2"`
}

type FeedEvent struct {
	ID uint64 `json:"id,string" gorm:"primaryKey;autoIncrement"`
	ContentEvent
}

type OutboxEvent struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"`
	ContentEvent
	Status      string     `gorm:"size:20;not null;index:idx_outbox_due,priority:1"`
	Attempts    int        `gorm:"not null;default:0"`
	NextRetryAt time.Time  `gorm:"type:datetime(6);not null;index:idx_outbox_due,priority:2"`
	LockedUntil *time.Time `gorm:"type:datetime(6)"`
	LockToken   string     `gorm:"size:36"`
	LastError   string     `gorm:"size:1000"`
	SentAt      *time.Time `gorm:"type:datetime(6)"`
	CreatedAt   time.Time
}

type FeedDeadLetter struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	Topic     string `gorm:"size:190;uniqueIndex:idx_feed_bad_message,priority:1"`
	Partition int    `gorm:"uniqueIndex:idx_feed_bad_message,priority:2"`
	Offset    int64  `gorm:"uniqueIndex:idx_feed_bad_message,priority:3"`
	Reason    string `gorm:"size:1000"`
	Payload   string `gorm:"type:text"`
	CreatedAt time.Time
}

type FeedAuthor struct {
	UserID    uint64 `json:"user_id,string"`
	UserName  string `json:"user_name"`
	AvatarURL string `json:"avatar_url"`
}

type FeedVideo struct {
	VideoID    uint64 `json:"video_id,string"`
	Title      string `json:"title"`
	CoverURL   string `json:"cover_url"`
	DurationMS uint64 `json:"duration_ms"`
}

type FollowingFeedItem struct {
	EventID     string     `json:"event_id"`
	Type        string     `json:"type"`
	PublishedAt time.Time  `json:"published_at"`
	Author      FeedAuthor `json:"author"`
	Video       *FeedVideo `json:"video,omitempty"`
	Post        *PostView  `json:"post,omitempty"`
}

type FollowingFeedPage struct {
	Items      []FollowingFeedItem `json:"items"`
	NextCursor string              `json:"next_cursor"`
	HasMore    bool                `json:"has_more"`
}
