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

	kafkago "github.com/segmentio/kafka-go"
)

const (
	defaultRealtimeCommitBatch = 100
	defaultRealtimeCommitWait  = 200 * time.Millisecond
	defaultLikeIdempotencyTTL  = 24 * time.Hour
)

type pendingLikeEvent struct {
	event   model.Message
	message kafkago.Message
}

// StartRealtimeConsumer 将 MySQL 排除在弹幕广播链路之外。点赞按房间缓冲，
// 并以聚合增量记录持久化。聚合数据和 Kafka 分区检查点先以原子方式提交，
// 随后依次推进 Redis 状态、发布/订阅，最后推进 Kafka 消费者偏移量。
func StartRealtimeConsumer(ctx context.Context) error {
	cfg := config.GlobalConfig.Kafka
	groupID := cfg.GroupID + "-realtime"
	batchSize, batchWait, idempotencyTTL := likeBatchSettings()
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     cfg.Brokers,
		GroupID:     groupID,
		Topic:       cfg.DanmuTopic,
		StartOffset: kafkago.LastOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
		// 实时消费者组针对延迟而不是批量大小优化。MaxWait=0 会使用 kafka-go
		// 默认的 10 秒等待时间，因此这里需要显式设置。
		MaxWait:          50 * time.Millisecond,
		ReadBatchTimeout: time.Second,
		// CommitInterval 非零时，CommitMessages 会变为异步执行。
		// 所有持久化或可见的副作用仍会在提交进入队列前完成，
		// 重放则由数据库检查点消化。
		CommitInterval: batchWait,
		ErrorLogger: kafkago.LoggerFunc(func(format string, args ...any) {
			log.Printf("kafka realtime reader: "+format, args...)
		}),
	})
	defer reader.Close()
	log.Printf(
		"danmu realtime consumer started, groupId=%s, topic=%s, like_batch=%d/%s",
		groupID,
		cfg.DanmuTopic,
		batchSize,
		batchWait,
	)

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

	ticker := time.NewTicker(batchWait)
	defer ticker.Stop()
	pendingCommits := make([]kafkago.Message, 0, batchSize)
	pendingLikes := make([]pendingLikeEvent, 0, batchSize)
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

	flushAll := func(flushCtx context.Context) error {
		if len(pendingLikes) > 0 {
			startedAt := time.Now()
			applied, err := applyLikeBatchesUntilSuccess(
				flushCtx,
				groupID,
				cfg.DanmuTopic,
				pendingLikes,
			)
			realtimeMetrics.likeBatchPersist.observe(time.Since(startedAt))
			if err != nil {
				return err
			}
			if err := syncLikeBatchRedisUntilSuccess(
				flushCtx,
				applied.CountsByRoom,
				pendingLikes,
				idempotencyTTL,
			); err != nil {
				return err
			}
			for roomID, count := range applied.CountsByRoom {
				dirtyLikeCounts[roomID] = count
			}
			realtimeMetrics.likeBatchTotal.Add(uint64(applied.BatchCount))
			realtimeMetrics.likeEventTotal.Add(uint64(applied.EventCount))
			pendingLikes = pendingLikes[:0]
		}
		if err := flushLikeStates(flushCtx, dirtyLikeCounts); err != nil {
			return err
		}
		return flushCommits(flushCtx)
	}

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := flushAll(flushCtx)
			cancel()
			return err
		case <-ticker.C:
			if err := flushAll(ctx); err != nil {
				return err
			}
		case result, ok := <-fetched:
			if !ok {
				return flushAll(ctx)
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
				if len(pendingCommits) >= batchSize {
					if err := flushAll(ctx); err != nil {
						return err
					}
					ticker.Reset(batchWait)
				}
				continue
			}
			enrichMessageFromKafka(&event, result.message)
			if event.Type == model.MsgTypeLike {
				pendingLikes = append(pendingLikes, pendingLikeEvent{
					event:   event,
					message: result.message,
				})
				pendingCommits = append(pendingCommits, result.message)
				if len(pendingLikes) >= batchSize || len(pendingCommits) >= batchSize {
					if err := flushAll(ctx); err != nil {
						return err
					}
					ticker.Reset(batchWait)
				}
				continue
			}
			if event.Type != model.MsgTypeDanma {
				pendingCommits = append(pendingCommits, result.message)
				if len(pendingCommits) >= batchSize {
					if err := flushAll(ctx); err != nil {
						return err
					}
					ticker.Reset(batchWait)
				}
				continue
			}
			if err := publishDanmaRealtimeUntilSuccess(ctx, event); err != nil {
				return err
			}
			pendingCommits = append(pendingCommits, result.message)
			if len(pendingCommits) >= batchSize {
				if err := flushAll(ctx); err != nil {
					return err
				}
				ticker.Reset(batchWait)
			}
		}
	}
}

func likeBatchSettings() (batchSize int, batchWait time.Duration, idempotencyTTL time.Duration) {
	batchSize = defaultRealtimeCommitBatch
	batchWait = defaultRealtimeCommitWait
	idempotencyTTL = defaultLikeIdempotencyTTL
	if config.GlobalConfig == nil {
		return
	}
	likeConfig := config.GlobalConfig.Danmaku.Like
	if likeConfig.BatchSize > 0 {
		batchSize = likeConfig.BatchSize
	}
	if likeConfig.BatchWaitMilliseconds > 0 {
		batchWait = time.Duration(likeConfig.BatchWaitMilliseconds) * time.Millisecond
	}
	if likeConfig.IdempotencyTTLSeconds > 0 {
		idempotencyTTL = time.Duration(likeConfig.IdempotencyTTLSeconds) * time.Second
	}
	return
}

func applyLikeBatchesUntilSuccess(
	ctx context.Context,
	groupID string,
	defaultTopic string,
	pending []pendingLikeEvent,
) (repo.LikeBatchApplyResult, error) {
	events := make([]repo.LikeAggregationEvent, 0, len(pending))
	for _, item := range pending {
		topic := item.message.Topic
		if topic == "" {
			topic = defaultTopic
		}
		events = append(events, repo.LikeAggregationEvent{
			Topic:     topic,
			Partition: item.message.Partition,
			Offset:    item.message.Offset,
			RoomID:    item.event.RoomID,
		})
	}
	for attempt := 1; ; attempt++ {
		applied, err := repo.ApplyLikeBatches(ctx, groupID, events)
		if err == nil {
			return applied, nil
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("persist aggregate like batches failed, attempt=%d, count=%d, err=%v", attempt, len(events), err)
		}
		select {
		case <-ctx.Done():
			return repo.LikeBatchApplyResult{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func syncLikeBatchRedisUntilSuccess(
	ctx context.Context,
	counts map[uint64]int64,
	pending []pendingLikeEvent,
	idempotencyTTL time.Duration,
) error {
	for attempt := 1; ; attempt++ {
		err := dao.SetRoomLikeCounts(ctx, counts)
		if err == nil {
			break
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("sync aggregate room like counts failed, rooms=%d attempt=%d: %v", len(counts), attempt, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}

	identities := make([]dao.RoomLikeIdentity, 0, len(pending))
	for _, item := range pending {
		identities = append(identities, dao.RoomLikeIdentity{
			MessageID: item.event.MessageID,
			RoomID:    item.event.RoomID,
			UserID:    item.event.UserID,
		})
	}
	for attempt := 1; ; attempt++ {
		conflicts, err := dao.ConfirmRoomLikes(ctx, identities, idempotencyTTL)
		if err == nil {
			if conflicts > 0 {
				// 聚合数据已经持久化，因此过期或冲突的 Redis 标识不能阻塞整个分区。
				// 保留 UUID 的现有归属，并继续提交 Kafka 区间。
				log.Printf("aggregate like idempotency marker conflicts, count=%d", conflicts)
			}
			return nil
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("confirm aggregate like idempotency markers failed, attempt=%d, count=%d: %v", attempt, len(identities), err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
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

func publishDanmaRealtimeUntilSuccess(ctx context.Context, message model.Message) error {
	cfg := config.GlobalConfig.Danmaku.RecentCache
	ttl := time.Duration(cfg.TTLSeconds) * time.Second
	hotMetricTTL := time.Duration(0)
	if config.GlobalConfig.Danmaku.HotRanking.Enabled {
		hotMetricTTL = time.Duration(config.GlobalConfig.Danmaku.HotRanking.MetricBucketTTLSeconds) * time.Second
		if hotMetricTTL <= 0 {
			hotMetricTTL = 3 * time.Minute
		}
	}
	startedAt := time.Now()
	for attempt := 1; ; attempt++ {
		err := dao.CacheAndPublishRecentDanma(
			ctx,
			dao.DanmuChannel,
			message,
			cfg.Size,
			ttl,
			hotMetricTTL,
		)
		if err == nil {
			realtimeMetrics.redisPublish.observe(time.Since(startedAt))
			realtimeMetrics.publishedTotal.Add(1)
			return nil
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("cache and publish realtime danmu failed, attempt=%d: %v", attempt, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
