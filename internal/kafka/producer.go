package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"live-stream/internal/config"
	"live-stream/internal/model"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

var writer *kafka.Writer

func InitKafkaProducer() error {
	brokers := config.GlobalConfig.Kafka.Brokers
	topic := config.GlobalConfig.Kafka.DanmuTopic
	if len(brokers) == 0 {
		return fmt.Errorf("kafka brokers cannot be empty")
	}
	if topic == "" {
		return fmt.Errorf("kafka danmu topic cannot be empty")
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := kafka.DialLeader(probeCtx, "tcp", brokers[0], topic, 0)
	if err != nil {
		return fmt.Errorf("connect to kafka topic leader: %w", err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("close kafka probe connection: %w", err)
	}

	writer = &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		Async:        true,
		RequiredAcks: kafka.RequireAll,
	}
	fmt.Printf("kafka writer init success, broker=%s, topic=%s\n", brokers[0], topic)
	return nil
}

func SendMsg(ctx context.Context, key []byte, data any) error {
	if writer == nil {
		return fmt.Errorf("kafka writer not initialized")
	}
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("json marshal err: %v", err)
	}
	err = writer.WriteMessages(ctx,
		kafka.Message{
			Key:   key,
			Value: bytes,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to write messages: %v", err)
	}

	return nil
}

func SendDanmaMsg(ctx context.Context, msg model.Message) error {
	if writer == nil {
		return fmt.Errorf("kafka writer not initialized")
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("json marshal err: %v", err)
	}
	key := fmt.Appendf(nil, "%d", msg.RoomID)
	return writer.WriteMessages(ctx,
		kafka.Message{
			Key:   key,
			Value: bytes,
		},
	)
}

func Close() {
	if writer != nil {
		_ = writer.Close()
		log.Println("kafka producer closed")
	}
}
