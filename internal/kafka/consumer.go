package kafka

import (
	"context"
	"encoding/json"
	"live-stream/internal/config"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"time"

	"log"

	"github.com/segmentio/kafka-go"
)

func commitWithRetry(ctx context.Context, reader *kafka.Reader, msg kafka.Message) error {
	for {
		err := reader.CommitMessages(ctx, msg)
		if err == nil {
			log.Printf(
				"commit kafka message success, partition=%d offset=%d",
				msg.Partition,
				msg.Offset,
			)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf(
			"commit kafka message failed, partition=%d offset=%d: %v",
			msg.Partition,
			msg.Offset,
			err,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}

	}
}

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
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			// 上下文取消，退出
			if ctx.Err() != nil {
				log.Println("kafka consumer context canceled, exiting")
				return nil
			}
			log.Printf("read kafka message err: %v\n", err)
			continue
		}
		var dm model.Danma
		err = json.Unmarshal(msg.Value, &dm)
		if err != nil {
			log.Printf(
				"invalid kafka message, topic=%s partition=%d offset=%d: %v",
				msg.Topic,
				msg.Partition,
				msg.Offset,
				err,
			)
			if err := commitWithRetry(ctx, reader, msg); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			continue
		}

		for {
			_, err := repo.CreateDanma(ctx, &dm)
			if err == nil {
				log.Printf(
					"save danmu success, partition=%d offset=%d",
					msg.Partition,
					msg.Offset,
				)
				break
			}
			log.Printf(
				"save danmu failed, partition=%d offset=%d: %v",
				msg.Partition,
				msg.Offset,
				err,
			)

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
		}

		err = commitWithRetry(ctx, reader, msg)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

	}

}
