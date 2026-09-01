package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

func commitWithRetry(ctx context.Context, reader *kafka.Reader, msgs ...kafka.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	lastMsg := msgs[len(msgs)-1]
	maxRetry := 5
	for i := 0; i < maxRetry; i++ {
		err := reader.CommitMessages(ctx, msgs...)
		if err == nil {
			log.Printf(
				"commit kafka messages success, count=%d, last_partition=%d, last_offset=%d",
				len(msgs),
				lastMsg.Partition,
				lastMsg.Offset,
			)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf(
			"commit kafka messages failed, count=%d, last_partition=%d, last_offset=%d: %v",
			len(msgs),
			lastMsg.Partition,
			lastMsg.Offset,
			err,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}

	}
	return fmt.Errorf("commit failed after %d retries, count=%d", maxRetry, len(msgs))
}

func commitUntilSuccess(ctx context.Context, reader *kafka.Reader, msgs ...kafka.Message) error {
	for {
		if err := commitWithRetry(ctx, reader, msgs...); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func StartKafkaConsumer(ctx context.Context) error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:          config.GlobalConfig.Kafka.Brokers,
		GroupID:          config.GlobalConfig.Kafka.GroupID,
		Topic:            config.GlobalConfig.Kafka.DanmuTopic,
		MinBytes:         1,
		MaxBytes:         10e6,
		MaxWait:          time.Second,
		ReadBatchTimeout: 5 * time.Second,
		ErrorLogger: kafka.LoggerFunc(func(format string, args ...any) {
			log.Printf("kafka reader: "+format, args...)
		}),
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
	)
	var (
		rawMsgs = make([]model.Message, 0, batchSize)
		msgs    = make([]kafka.Message, 0, batchSize)
		ticker  = time.NewTicker(maxWait)
	)
	defer ticker.Stop()

	type fetchResult struct {
		msg kafka.Message
		err error
	}
	fetched := make(chan fetchResult)
	go func() {
		defer close(fetched)
		for {
			msg, err := reader.FetchMessage(ctx)
			select {
			case fetched <- fetchResult{msg: msg, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil && ctx.Err() != nil {
				return
			}
		}
	}()

	flush := func(processCtx context.Context) {
		if len(msgs) == 0 {
			return
		}
		processBatch(processCtx, reader, rawMsgs, msgs)
		msgs = msgs[:0]
		rawMsgs = rawMsgs[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			flush(flushCtx)
			cancel()
			return nil
		case <-ticker.C:
			flush(ctx)
		case result, ok := <-fetched:
			if !ok {
				return nil
			}
			if result.err != nil {
				// 上下文取消，退出
				if ctx.Err() != nil {
					log.Println("kafka consumer context canceled, exiting")
					return nil
				}
				if !errors.Is(result.err, context.Canceled) && !errors.Is(result.err, context.DeadlineExceeded) {
					log.Printf("read kafka message err: %v\n", result.err)
				}
				continue
			}
			msg := result.msg
			// 反序列化成业务消息
			var danmaMsg model.Message
			err := json.Unmarshal(msg.Value, &danmaMsg)
			if err != nil {
				log.Printf(
					"invalid kafka message, topic=%s partition=%d offset=%d: %v",
					msg.Topic,
					msg.Partition,
					msg.Offset,
					err,
				)
				_ = commitUntilSuccess(ctx, reader, msg)
				continue
			}
			rawMsgs = append(rawMsgs, danmaMsg)
			msgs = append(msgs, msg)
			if len(rawMsgs) >= batchSize {
				flush(ctx)
				ticker.Reset(maxWait)
			}
		}
	}
}

func processBatch(ctx context.Context, reader *kafka.Reader, rawMsgs []model.Message, msgs []kafka.Message) {
	for attempt := 1; ; attempt++ {
		if err := repo.PersistDanmaEvents(ctx, rawMsgs); err == nil {
			break
		} else {
			log.Printf("persist danma transaction failed, attempt=%d, count=%d, err=%v", attempt, len(rawMsgs), err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}

	// 点赞消息：更新 Redis 房间点赞计数
	for _, msg := range rawMsgs {
		if msg.Type == model.MsgTypeLike {
			dao.IncrRoomLikeCount(ctx, msg.RoomID)
		}
	}

	// MySQL 中已经同时保存了 outbox，因此此处可以提交 offset。Redis
	// 发布由独立 dispatcher 重试，避免数据库成功但发布前崩溃造成丢失。
	if len(msgs) > 0 {
		_ = commitUntilSuccess(ctx, reader, msgs...)
	}
}
