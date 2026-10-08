package model

import "time"

// LikeBatch 表示某个房间在已消费 Kafka 分区区间内的一条持久化聚合记录。
// 单次点赞事件保留在 Kafka 中；MySQL 仅保存恢复和审计所需的聚合增量。
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

// LikeAggregationCheckpoint 用于保证以下场景中的聚合持久化安全：MySQL 已提交，
// 但消费者偏移量尚未提交，导致 Kafka 重放一段数据。该记录会与 LikeBatch 和
// Room 在同一个事务中锁定并推进。
type LikeAggregationCheckpoint struct {
	ConsumerGroup string    `json:"consumer_group" gorm:"primaryKey;size:128"`
	Topic         string    `json:"topic" gorm:"primaryKey;size:128"`
	Partition     int       `json:"partition" gorm:"primaryKey;column:kafka_partition"`
	LastOffset    int64     `json:"last_offset" gorm:"not null;default:-1"`
	UpdatedAt     time.Time `json:"updated_at"`
}
