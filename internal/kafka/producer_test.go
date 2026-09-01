package kafka

import (
	"testing"
	"time"
)

func TestRealtimeProducerUsesLowLatencyBatchTimeout(t *testing.T) {
	writer := newWriter([]string{"127.0.0.1:9092"}, "danmu_topic")
	if writer.BatchTimeout != 5*time.Millisecond {
		t.Fatalf("batch timeout = %s, want 5ms", writer.BatchTimeout)
	}
	if writer.BatchSize != 100 {
		t.Fatalf("batch size = %d, want 100", writer.BatchSize)
	}
	if writer.Async {
		t.Fatal("producer must wait for Kafka acknowledgement")
	}
}
