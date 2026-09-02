package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"live-stream/internal/model"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const recentDanmaKeyPrefix = "room:recent:"

var cacheAndPublishRecentDanmaScript = redis.NewScript(`
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[2])
local count = redis.call('ZCARD', KEYS[1])
local max_items = tonumber(ARGV[3])
if count > max_items then
  redis.call('ZREMRANGEBYRANK', KEYS[1], 0, count - max_items - 1)
end
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[4]))
return redis.call('PUBLISH', ARGV[5], ARGV[2])
`)

func recentDanmaKey(roomID uint64) string {
	return recentDanmaKeyPrefix + strconv.FormatUint(roomID, 10)
}

// CacheAndPublishRecentDanma atomically stores the latest recoverable event
// before publishing it. A subscriber can therefore start replay immediately
// after receiving a resync hint without waiting for MySQL persistence.
func CacheAndPublishRecentDanma(
	ctx context.Context,
	channel string,
	message model.Message,
	maxItems int,
	ttl time.Duration,
) error {
	if maxItems <= 0 {
		payload, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("marshal realtime danmaku: %w", err)
		}
		return Publish(ctx, channel, string(payload))
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal recent danmaku: %w", err)
	}
	if message.RoomSeq == 0 {
		return fmt.Errorf("cache recent danmaku without room_seq")
	}
	return cacheAndPublishRecentDanmaScript.Run(
		ctx,
		SC.Redis,
		[]string{recentDanmaKey(message.RoomID)},
		message.RoomSeq,
		string(payload),
		maxItems,
		int64(ttl/time.Second),
		channel,
	).Err()
}

// SelectRecentDanmasAfterSeq returns covered=false when the requested cursor
// is older than the retained Redis window. Callers must then fall back to DB.
func SelectRecentDanmasAfterSeq(
	ctx context.Context,
	roomID uint64,
	afterSeq uint64,
	limit int,
) (messages []model.Danma, covered bool, err error) {
	if limit <= 0 {
		return nil, false, nil
	}
	key := recentDanmaKey(roomID)
	oldest, err := SC.Redis.ZRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		return nil, false, fmt.Errorf("read recent danmaku floor: %w", err)
	}
	if len(oldest) == 0 || afterSeq < uint64(oldest[0].Score) {
		return nil, false, nil
	}

	values, err := SC.Redis.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     key,
		Start:   "(" + strconv.FormatUint(afterSeq, 10),
		Stop:    "+inf",
		ByScore: true,
		Offset:  0,
		Count:   int64(limit),
	}).Result()
	if err != nil {
		return nil, false, fmt.Errorf("read recent danmaku range: %w", err)
	}

	messages = make([]model.Danma, 0, len(values))
	for _, value := range values {
		var message model.Message
		if err := json.Unmarshal([]byte(value), &message); err != nil {
			return nil, false, fmt.Errorf("decode cached danmaku: %w", err)
		}
		createdAt := time.Now()
		if message.CreatedAt != nil {
			createdAt = *message.CreatedAt
		}
		messages = append(messages, model.Danma{
			MessageID: message.MessageID,
			RoomID:    message.RoomID,
			RoomSeq:   message.RoomSeq,
			UserID:    message.UserID,
			UserName:  message.UserName,
			Content:   message.Content,
			CreatedAt: createdAt,
		})
	}
	return messages, true, nil
}
