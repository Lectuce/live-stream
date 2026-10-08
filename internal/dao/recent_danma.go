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
local max_items = tonumber(ARGV[3])
local added = 1
if max_items > 0 then
  added = redis.call('ZADD', KEYS[1], 'NX', ARGV[1], ARGV[2])
  local count = redis.call('ZCARD', KEYS[1])
  if count > max_items then
    redis.call('ZREMRANGEBYRANK', KEYS[1], 0, count - max_items - 1)
  end
  redis.call('EXPIRE', KEYS[1], tonumber(ARGV[4]))
end
if tonumber(ARGV[6]) > 0 and added == 1 then
  redis.call('INCR', KEYS[2])
  redis.call('EXPIRE', KEYS[2], tonumber(ARGV[6]))
end
return redis.call('PUBLISH', ARGV[5], ARGV[2])
`)

func recentDanmaKey(roomID uint64) string {
	return recentDanmaKeyPrefix + strconv.FormatUint(roomID, 10)
}

// CacheAndPublishRecentDanma 在发布前以原子方式保存最新的可恢复事件。
// 因此订阅者收到重新同步提示后可立即开始重放，无需等待 MySQL 持久化。
func CacheAndPublishRecentDanma(
	ctx context.Context,
	channel string,
	message model.Message,
	maxItems int,
	ttl time.Duration,
	hotMetricTTL time.Duration,
) error {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal recent danmaku: %w", err)
	}
	if maxItems > 0 && message.RoomSeq == 0 {
		return fmt.Errorf("cache recent danmaku without room_seq")
	}
	return cacheAndPublishRecentDanmaScript.Run(
		ctx,
		SC.Redis,
		[]string{
			recentDanmaKey(message.RoomID),
			hotMetricMinuteKey("danmas", message.RoomID, time.Now().Unix()/60),
		},
		message.RoomSeq,
		string(payload),
		maxItems,
		int64(ttl/time.Second),
		channel,
		int64(hotMetricTTL/time.Second),
	).Err()
}

// SelectRecentDanmasAfterSeq 在请求的游标早于 Redis 保留窗口时返回
// covered=false，此时调用方必须回退到数据库查询。
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
