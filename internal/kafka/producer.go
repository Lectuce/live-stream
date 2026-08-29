package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"live-stream/internal/config"
	"live-stream/internal/model"
	"log"

	"github.com/segmentio/kafka-go"
)

var writer *kafka.Writer

func InitKafkaProducer() error {
	brokers := config.GlobalConfig.Kafka.Brokers
	writer = &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        config.GlobalConfig.Kafka.DanmuTopic,
		Balancer:     &kafka.LeastBytes{},
		Async:        true,
		RequiredAcks: kafka.RequireAll,
	}
	fmt.Println("kafka writer init success")
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
