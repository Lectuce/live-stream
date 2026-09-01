package kafka

import (
	"live-stream/internal/model"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

func TestEnrichMessageFromKafkaUsesPartitionOffsetAsCursor(t *testing.T) {
	createdAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	event := model.Message{RoomID: 42}
	enrichMessageFromKafka(&event, kafkago.Message{Partition: 2, Offset: 99, Time: createdAt})

	if event.RoomSeq != 100 {
		t.Fatalf("room sequence = %d, want 100", event.RoomSeq)
	}
	if event.CreatedAt == nil || !event.CreatedAt.Equal(createdAt) {
		t.Fatalf("created at = %v, want %v", event.CreatedAt, createdAt)
	}
}

func TestEnrichMessageFromKafkaPreservesProducerTimestamp(t *testing.T) {
	producerTime := time.Date(2026, time.September, 1, 11, 0, 0, 0, time.UTC)
	event := model.Message{CreatedAt: &producerTime}
	enrichMessageFromKafka(&event, kafkago.Message{Offset: 8, Time: producerTime.Add(time.Hour)})

	if event.RoomSeq != 9 {
		t.Fatalf("room sequence = %d, want 9", event.RoomSeq)
	}
	if event.CreatedAt == nil || !event.CreatedAt.Equal(producerTime) {
		t.Fatalf("producer timestamp was replaced: %v", event.CreatedAt)
	}
}
