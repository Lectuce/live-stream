package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"log"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

const (
	realtimeCommitBatch = 100
	realtimeCommitWait  = 200 * time.Millisecond
)

// StartRealtimeConsumer keeps MySQL completely outside the live broadcast
// path. Kafka is the durable hand-off; Redis publication is retried before the
// realtime consumer offset is committed. A crash after Publish and before
// Commit can duplicate an event, which clients remove by message_id.
func StartRealtimeConsumer(ctx context.Context) error {
	cfg := config.GlobalConfig.Kafka
	groupID := cfg.GroupID + "-realtime"
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     cfg.Brokers,
		GroupID:     groupID,
		Topic:       cfg.DanmuTopic,
		StartOffset: kafkago.LastOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
		// The realtime group optimizes for latency, not batch size. MaxWait=0
		// would select kafka-go's 10 second default, so keep this explicit.
		MaxWait:          50 * time.Millisecond,
		ReadBatchTimeout: time.Second,
		// CommitMessages becomes asynchronous when CommitInterval is non-zero.
		// Redis publication still happens first, but coordinator round trips no
		// longer pause the realtime fetch/publish loop.
		CommitInterval: realtimeCommitWait,
		ErrorLogger: kafkago.LoggerFunc(func(format string, args ...any) {
			log.Printf("kafka realtime reader: "+format, args...)
		}),
	})
	defer reader.Close()
	log.Printf("danmu realtime consumer started, groupId=%s, topic=%s", groupID, cfg.DanmuTopic)

	type fetchResult struct {
		message kafkago.Message
		err     error
	}
	fetched := make(chan fetchResult)
	go func() {
		defer close(fetched)
		for {
			message, err := reader.FetchMessage(ctx)
			select {
			case fetched <- fetchResult{message: message, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil && ctx.Err() != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(realtimeCommitWait)
	defer ticker.Stop()
	pendingCommits := make([]kafkago.Message, 0, realtimeCommitBatch)
	dirtyLikeCounts := make(map[uint64]int64)
	flushCommits := func(commitCtx context.Context) error {
		if len(pendingCommits) == 0 {
			return nil
		}
		startedAt := time.Now()
		if err := commitUntilSuccess(commitCtx, reader, pendingCommits...); err != nil {
			realtimeMetrics.commitEnqueue.observe(time.Since(startedAt))
			return err
		}
		realtimeMetrics.commitEnqueue.observe(time.Since(startedAt))
		realtimeMetrics.commitBatchTotal.Add(1)
		pendingCommits = pendingCommits[:0]
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := flushLikeStates(flushCtx, dirtyLikeCounts)
			if err == nil {
				err = flushCommits(flushCtx)
			}
			cancel()
			return err
		case <-ticker.C:
			if err := flushLikeStates(ctx, dirtyLikeCounts); err != nil {
				return err
			}
			if err := flushCommits(ctx); err != nil {
				return err
			}
		case result, ok := <-fetched:
			if !ok {
				return flushCommits(ctx)
			}
			if result.err != nil {
				if ctx.Err() != nil {
					return nil
				}
				if !errors.Is(result.err, context.Canceled) && !errors.Is(result.err, context.DeadlineExceeded) {
					log.Printf("read realtime kafka message err: %v", result.err)
				}
				continue
			}
			realtimeMetrics.fetchedTotal.Add(1)
			if !result.message.Time.IsZero() {
				realtimeMetrics.messageAge.observe(time.Since(result.message.Time))
			}

			var event model.Message
			if err := json.Unmarshal(result.message.Value, &event); err != nil {
				log.Printf(
					"invalid realtime kafka message, partition=%d offset=%d: %v",
					result.message.Partition,
					result.message.Offset,
					err,
				)
				pendingCommits = append(pendingCommits, result.message)
				if len(pendingCommits) >= realtimeCommitBatch {
					if err := flushCommits(ctx); err != nil {
						return err
					}
					ticker.Reset(realtimeCommitWait)
				}
				continue
			}
			enrichMessageFromKafka(&event, result.message)
			// Likes are aggregated in Redis and emitted as a replaceable state
			// update. They must not create one room-wide event per click.
			if event.Type == model.MsgTypeLike {
				count, err := incrementLikeUntilSuccess(ctx, event.RoomID)
				if err != nil {
					return err
				}
				dirtyLikeCounts[event.RoomID] = count
				pendingCommits = append(pendingCommits, result.message)
				if len(pendingCommits) >= realtimeCommitBatch {
					if err := flushLikeStates(ctx, dirtyLikeCounts); err != nil {
						return err
					}
					if err := flushCommits(ctx); err != nil {
						return err
					}
					ticker.Reset(realtimeCommitWait)
				}
				continue
			}
			if event.Type != model.MsgTypeDanma {
				pendingCommits = append(pendingCommits, result.message)
				if len(pendingCommits) >= realtimeCommitBatch {
					if err := flushCommits(ctx); err != nil {
						return err
					}
					ticker.Reset(realtimeCommitWait)
				}
				continue
			}
			payload, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("marshal realtime event %s: %w", event.MessageID, err)
			}
			if err := publishRealtimeUntilSuccess(ctx, string(payload)); err != nil {
				return err
			}
			pendingCommits = append(pendingCommits, result.message)
			if len(pendingCommits) >= realtimeCommitBatch {
				if err := flushCommits(ctx); err != nil {
					return err
				}
				ticker.Reset(realtimeCommitWait)
			}
		}
	}
}

func incrementLikeUntilSuccess(ctx context.Context, roomID uint64) (int64, error) {
	for attempt := 1; ; attempt++ {
		count, err := dao.IncrRoomLikeCount(ctx, roomID)
		if err == nil {
			return count, nil
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("increment room likes failed, room=%d attempt=%d: %v", roomID, attempt, err)
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func flushLikeStates(ctx context.Context, dirty map[uint64]int64) error {
	for roomID, count := range dirty {
		event := model.Message{
			Type:   model.MsgTypeSystem,
			RoomID: roomID,
			Data: map[string]any{
				"sub_type":   "like_count",
				"like_count": count,
			},
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal like state for room %d: %w", roomID, err)
		}
		if err := publishRealtimeUntilSuccess(ctx, string(payload)); err != nil {
			return fmt.Errorf("publish like state for room %d: %w", roomID, err)
		}
		delete(dirty, roomID)
	}
	return nil
}

func publishRealtimeUntilSuccess(ctx context.Context, payload string) error {
	startedAt := time.Now()
	for attempt := 1; ; attempt++ {
		if err := dao.Publish(ctx, dao.DanmuChannel, payload); err == nil {
			realtimeMetrics.redisPublish.observe(time.Since(startedAt))
			realtimeMetrics.publishedTotal.Add(1)
			return nil
		} else if attempt == 1 || attempt%10 == 0 {
			log.Printf("publish realtime danmu failed, attempt=%d: %v", attempt, err)
		}

		delay := 100 * time.Millisecond
		if attempt < 7 {
			delay *= time.Duration(1 << (attempt - 1))
		} else {
			delay = 6400 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}
