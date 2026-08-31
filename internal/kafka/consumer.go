package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

func commitWithRetry(ctx context.Context, reader *kafka.Reader, msg kafka.Message) error {
	maxRetry := 5
	for i := 0; i < maxRetry; i++ {
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
	return fmt.Errorf("commit failed after %d retries, partition=%d offset=%d", maxRetry, msg.Partition, msg.Offset)
}

func StartKafkaConsumer(ctx context.Context) error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  config.GlobalConfig.Kafka.Brokers,
		GroupID:  config.GlobalConfig.Kafka.GroupID,
		Topic:    config.GlobalConfig.Kafka.DanmuTopic,
		MinBytes: 10e3,
		MaxBytes: 10e6,
		MaxWait:  100 * time.Millisecond,
	})
	defer func() {
		err := reader.Close()
		if err != nil {
			log.Printf("kafka reader close err: %v\n", err)
		}
	}()
	log.Printf("danmu consumer started, groupId=%s, topic=%s", config.GlobalConfig.Kafka.GroupID, config.GlobalConfig.Kafka.DanmuTopic)
	const (
		batchSize = 100
		maxWait   = 200 * time.Millisecond
		maxRetry  = 3
	)
	var (
		batch   = make([]model.Danma, 0, batchSize)
		rawMsgs = make([]model.Message, 0, batchSize)
		msgs    = make([]kafka.Message, 0, batchSize)
		ticker  = time.NewTicker(maxWait)
	)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if len(msgs) > 0 {
				processBatch(ctx, reader, batch, rawMsgs, msgs, maxRetry)
				batch = batch[:0]
				msgs = msgs[:0]
				rawMsgs = rawMsgs[:0]
			}
		default:
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
			// 反序列化成业务消息
			var danmaMsg model.Message
			err = json.Unmarshal(msg.Value, &danmaMsg)
			if err != nil {
				log.Printf(
					"invalid kafka message, topic=%s partition=%d offset=%d: %v",
					msg.Topic,
					msg.Partition,
					msg.Offset,
					err,
				)
				_ = commitWithRetry(ctx, reader, msg)
				continue
			}
			if danmaMsg.Type == model.MsgTypeDanma {
				// 转换为数据库实体
				dbDanma := model.Danma{
					MessageID: danmaMsg.MessageID,
					RoomID:    danmaMsg.RoomID,
					UserID:    danmaMsg.UserID,
					UserName:  danmaMsg.UserName,
					Content:   danmaMsg.Content,
				}
				batch = append(batch, dbDanma)
			}

			rawMsgs = append(rawMsgs, danmaMsg)
			msgs = append(msgs, msg)
			if len(rawMsgs) >= batchSize {
				processBatch(ctx, reader, batch, rawMsgs, msgs, maxRetry)
				batch = batch[:0]
				msgs = msgs[:0]
				rawMsgs = rawMsgs[:0]
				ticker.Reset(maxWait)
			}

		}
	}
}

func processBatch(ctx context.Context, reader *kafka.Reader, batch []model.Danma, rawMsgs []model.Message, msgs []kafka.Message, maxRetry int) {

	if len(batch) > 0 {
		var insertErr error

		for attempt := 0; attempt < maxRetry; attempt++ {
			insertErr = repo.BatchCreateDanma(ctx, batch)

			// 成功：立即结束重试，继续 Redis 发布和 offset 提交
			if insertErr == nil {
				break
			}

			log.Printf(
				"batch insert failed, attempt=%d/%d, count=%d, err=%v",
				attempt+1,
				maxRetry,
				len(batch),
				insertErr,
			)

			// 最后一次仍失败：不能发布 Redis，也不能提交 offset
			if attempt == maxRetry-1 {
				log.Printf(
					"batch insert finally failed, count=%d, err=%v",
					len(batch),
					insertErr,
				)
				return
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}

	// 点赞消息：更新 Redis 房间点赞计数
	for _, msg := range rawMsgs {
		if msg.Type == model.MsgTypeLike {
			dao.IncrRoomLikeCount(ctx, msg.RoomID)
		}
	}

	// 批量发布 Redis
	for _, dm := range batch {
		payload, _ := json.Marshal(dm)
		_ = dao.Publish(ctx, dao.DanmuChannel, string(payload))
	}

	// 批量提交 offset（提交最后一条）
	if len(msgs) > 0 {
		lastMsg := msgs[len(msgs)-1]
		_ = commitWithRetry(ctx, reader, lastMsg)
	}
}
