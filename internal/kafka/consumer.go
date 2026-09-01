package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"live-stream/internal/config"
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
			enrichMessageFromKafka(&danmaMsg, msg)
			rawMsgs = append(rawMsgs, danmaMsg)
			msgs = append(msgs, msg)
			if len(rawMsgs) >= batchSize {
				flush(ctx)
				ticker.Reset(maxWait)
			}
		}
	}
}

// Kafka keying keeps every room on one partition. The partition offset is
// therefore a stable, monotonically increasing cursor for that room. Other
// rooms sharing the partition can create gaps, so clients compare/order by the
// cursor but must not assume every next value is exactly previous+1.
func enrichMessageFromKafka(message *model.Message, kafkaMessage kafka.Message) {
	if kafkaMessage.Offset >= 0 {
		message.RoomSeq = uint64(kafkaMessage.Offset) + 1
	}
	if message.CreatedAt == nil {
		createdAt := kafkaMessage.Time
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		message.CreatedAt = &createdAt
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

	// MySQL 成功后再提交持久化 consumer 的 offset。实时广播由另一个
	// consumer group 独立完成，数据库抖动不会阻塞观众看到弹幕。
	if len(msgs) > 0 {
		_ = commitUntilSuccess(ctx, reader, msgs...)
	}
}
