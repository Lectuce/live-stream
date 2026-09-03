package model

import "time"

// LikeBatch is one durable aggregate for a room inside a consumed Kafka
// partition range. Individual click events stay in Kafka; MySQL stores only
// the aggregate delta required for recovery and auditing.
type LikeBatch struct {
	BatchID       string    `json:"batch_id" gorm:"primaryKey;size:64"`
	ConsumerGroup string    `json:"consumer_group" gorm:"size:128;not null;index:idx_like_batch_source"`
	Topic         string    `json:"topic" gorm:"size:128;not null;index:idx_like_batch_source"`
	Partition     int       `json:"partition" gorm:"column:kafka_partition;not null;index:idx_like_batch_source"`
	RoomID        uint64    `json:"room_id" gorm:"not null;index"`
	StartOffset   int64     `json:"start_offset" gorm:"not null"`
	EndOffset     int64     `json:"end_offset" gorm:"not null"`
	Delta         int64     `json:"delta" gorm:"not null"`
	CreatedAt     time.Time `json:"created_at" gorm:"not null"`
}

// LikeAggregationCheckpoint makes aggregate persistence safe when Kafka
// replays a range after MySQL committed but the consumer offset did not. The
// row is locked and advanced in the same transaction as LikeBatch and Room.
type LikeAggregationCheckpoint struct {
	ConsumerGroup string    `json:"consumer_group" gorm:"primaryKey;size:128"`
	Topic         string    `json:"topic" gorm:"primaryKey;size:128"`
	Partition     int       `json:"partition" gorm:"primaryKey;column:kafka_partition"`
	LastOffset    int64     `json:"last_offset" gorm:"not null;default:-1"`
	UpdatedAt     time.Time `json:"updated_at"`
}
