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

var DanmuOutChan = make(chan model.Danma, 1024)

func StartKafkaConsumer(ctx context.Context) error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  config.GlobalConfig.Kafka.Brokers,
		GroupID:  config.GlobalConfig.Kafka.GroupID,
		Topic:    config.GlobalConfig.Kafka.DanmuTopic,
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	defer func() {
		err := reader.Close()
		if err != nil {
			log.Printf("kafka reader close err: %v\n", err)
		}
	}()
	log.Printf("danmu consumer started, groupId=%s, topic=%s", config.GlobalConfig.Kafka.GroupID, config.GlobalConfig.Kafka.DanmuTopic)
	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			// 上下文取消，退出
			if ctx.Err() != nil {
				log.Println("kafka consumer context canceled, exiting")
				return nil
			}
			log.Printf("read kafka message err: %v\n", err)
			continue
		}
		fmt.Printf("message at topic/partition/offset %v/%v/%v: %s = %s\n", msg.Topic, msg.Partition, msg.Offset, string(msg.Key), string(msg.Value))
		var dm model.Danma
		err = json.Unmarshal(msg.Value, &dm)
		if err != nil {
			log.Printf("unmarshal danmu failed, raw=%s, err=%v", string(msg.Value), err)
			continue
		}

		DanmuOutChan <- dm

		log.Printf("consume danma: room=%d, user=%d, content=%s", dm.RoomID, dm.UserID, dm.Content)

	}

}
