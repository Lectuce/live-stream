package dao

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const HotRoomRankingKey = "rooms:hot"

type HotRoomRank struct {
	RoomID uint64
	Score  float64
}

type RecentRoomActivity struct {
	Likes  float64
	Danmas float64
}

const confirmAndRecordHotLikeLua = `
local current = redis.call('GET', KEYS[1])
if current and current ~= ARGV[1] and current ~= ARGV[2] then
  return {-1, 0}
end
redis.call('SET', KEYS[1], ARGV[2], 'EX', tonumber(ARGV[3]))
local user_count = redis.call('INCR', KEYS[2])
if user_count == 1 then
  redis.call('EXPIRE', KEYS[2], tonumber(ARGV[4]))
end
if user_count <= tonumber(ARGV[5]) then
  redis.call('INCR', KEYS[3])
  redis.call('EXPIRE', KEYS[3], tonumber(ARGV[4]))
  return {1, 1}
end
return {1, 0}
`

var confirmAndRecordHotLikeScript = redis.NewScript(confirmAndRecordHotLikeLua)

// ConfirmRoomLikeAndRecordHotContribution 将入口预留转换为已接受标记，
// 并以每个用户每分钟最多 N 次的频率计入近似排行信号。
// 持久化历史点赞计数不受此限制，由 Kafka 消费者独立更新。
func ConfirmRoomLikeAndRecordHotContribution(
	ctx context.Context,
	identity RoomLikeIdentity,
	acceptedTTL time.Duration,
	metricTTL time.Duration,
	maxPerUserMinute int,
	now time.Time,
) (contributed bool, err error) {
	if acceptedTTL <= 0 {
		acceptedTTL = 24 * time.Hour
	}
	if metricTTL <= 0 {
		metricTTL = 3 * time.Minute
	}
	if maxPerUserMinute <= 0 {
		maxPerUserMinute = 1<<31 - 1
	}
	accepted, pending := roomLikeReservationValues(identity)
	minute := now.Unix() / 60
	values, err := confirmAndRecordHotLikeScript.Run(
		ctx,
		SC.Redis,
		[]string{
			roomLikeIdempotencyKey(identity.MessageID),
			hotLikeUserMinuteKey(identity.RoomID, identity.UserID, minute),
			hotMetricMinuteKey("likes", identity.RoomID, minute),
		},
		pending,
		accepted,
		secondsAtLeastOne(acceptedTTL),
		secondsAtLeastOne(metricTTL),
		maxPerUserMinute,
	).Int64Slice()
	if err != nil {
		return false, err
	}
	if len(values) != 2 || values[0] != 1 {
		return false, fmt.Errorf("like message_id reservation belongs to another identity")
	}
	return values[1] == 1, nil
}

const reportRoomPresenceLua = `
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[1])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[4]))
redis.call('EXPIRE', KEYS[2], tonumber(ARGV[4]))
return 1
`

const removeRoomPresenceLua = `
redis.call('HDEL', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
`

// ReportLocalOnlineCounts 为每个网关实例和房间写入一条心跳。
// 上一次快照中存在但本次缺失的房间会立即删除；进程崩溃则由聚合时使用的
// 心跳截止时间处理。
func ReportLocalOnlineCounts(
	ctx context.Context,
	instanceID string,
	current map[uint64]int,
	previous map[uint64]int,
	ttl time.Duration,
	now time.Time,
) error {
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	nowMillis := now.UnixMilli()
	_, err := SC.Redis.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for roomID, count := range current {
			countsKey, heartbeatKey := roomPresenceKeys(roomID)
			pipe.Eval(
				ctx,
				reportRoomPresenceLua,
				[]string{countsKey, heartbeatKey},
				instanceID,
				count,
				nowMillis,
				secondsAtLeastOne(ttl),
			)
		}
		for roomID := range previous {
			if _, exists := current[roomID]; exists {
				continue
			}
			countsKey, heartbeatKey := roomPresenceKeys(roomID)
			pipe.Eval(ctx, removeRoomPresenceLua, []string{countsKey, heartbeatKey}, instanceID)
		}
		return nil
	})
	return err
}

const sumRoomPresenceLua = `
local total = 0
local instances = redis.call('HKEYS', KEYS[1])
for _, instance in ipairs(instances) do
  local heartbeat = redis.call('ZSCORE', KEYS[2], instance)
  if not heartbeat or tonumber(heartbeat) < tonumber(ARGV[1]) then
    redis.call('HDEL', KEYS[1], instance)
    redis.call('ZREM', KEYS[2], instance)
  else
    local count = redis.call('HGET', KEYS[1], instance)
    if count then total = total + tonumber(count) end
  end
end
return total
`

func ReadGlobalOnlineCounts(
	ctx context.Context,
	roomIDs []uint64,
	staleBefore time.Time,
) (map[uint64]int64, error) {
	counts := make(map[uint64]int64, len(roomIDs))
	commands := make(map[uint64]*redis.Cmd, len(roomIDs))
	_, err := SC.Redis.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, roomID := range roomIDs {
			countsKey, heartbeatKey := roomPresenceKeys(roomID)
			commands[roomID] = pipe.Eval(
				ctx,
				sumRoomPresenceLua,
				[]string{countsKey, heartbeatKey},
				staleBefore.UnixMilli(),
			)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for roomID, command := range commands {
		count, err := command.Int64()
		if err != nil {
			return nil, err
		}
		counts[roomID] = count
	}
	return counts, nil
}

// ReadRecentRoomActivity 使用当前分钟桶和上一个桶中仍与窗口重叠的部分，
// 近似计算过去一分钟的活动量。
func ReadRecentRoomActivity(
	ctx context.Context,
	roomIDs []uint64,
	now time.Time,
) (map[uint64]RecentRoomActivity, error) {
	if len(roomIDs) == 0 {
		return map[uint64]RecentRoomActivity{}, nil
	}
	minute := now.Unix() / 60
	keys := make([]string, 0, len(roomIDs)*4)
	for _, roomID := range roomIDs {
		keys = append(keys,
			hotMetricMinuteKey("likes", roomID, minute),
			hotMetricMinuteKey("likes", roomID, minute-1),
			hotMetricMinuteKey("danmas", roomID, minute),
			hotMetricMinuteKey("danmas", roomID, minute-1),
		)
	}
	values, err := SC.Redis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	activities := make(map[uint64]RecentRoomActivity, len(roomIDs))
	for index, roomID := range roomIDs {
		base := index * 4
		activities[roomID] = RecentRoomActivity{
			Likes:  blendMinuteBuckets(redisNumber(values[base]), redisNumber(values[base+1]), now),
			Danmas: blendMinuteBuckets(redisNumber(values[base+2]), redisNumber(values[base+3]), now),
		}
	}
	return activities, nil
}

var replaceHotRoomRankingScript = redis.NewScript(`
redis.call('DEL', KEYS[1])
for i = 1, #ARGV, 2 do
  redis.call('ZADD', KEYS[1], ARGV[i + 1], ARGV[i])
end
return #ARGV / 2
`)

func ReplaceHotRoomRanking(ctx context.Context, scores map[uint64]float64) error {
	args := make([]any, 0, len(scores)*2)
	for roomID, score := range scores {
		args = append(args, strconv.FormatUint(roomID, 10), score)
	}
	return replaceHotRoomRankingScript.Run(ctx, SC.Redis, []string{HotRoomRankingKey}, args...).Err()
}

func ReadHotRoomRanking(ctx context.Context, limit int) ([]HotRoomRank, error) {
	if limit <= 0 {
		return nil, nil
	}
	values, err := SC.Redis.ZRevRangeWithScores(ctx, HotRoomRankingKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	ranking := make([]HotRoomRank, 0, len(values))
	for _, value := range values {
		roomID, err := strconv.ParseUint(fmt.Sprint(value.Member), 10, 64)
		if err != nil {
			continue
		}
		ranking = append(ranking, HotRoomRank{RoomID: roomID, Score: value.Score})
	}
	return ranking, nil
}

func hotMetricMinuteKey(kind string, roomID uint64, minute int64) string {
	return "room:hot:" + kind + ":1m:" + strconv.FormatUint(roomID, 10) + ":" + strconv.FormatInt(minute, 10)
}

func hotLikeUserMinuteKey(roomID, userID uint64, minute int64) string {
	return "room:hot:likes:user:1m:" + strconv.FormatUint(roomID, 10) + ":" +
		strconv.FormatUint(userID, 10) + ":" + strconv.FormatInt(minute, 10)
}

func roomPresenceKeys(roomID uint64) (counts string, heartbeats string) {
	suffix := strconv.FormatUint(roomID, 10)
	return "room:online:nodes:" + suffix, "room:online:heartbeats:" + suffix
}

func secondsAtLeastOne(duration time.Duration) int64 {
	seconds := int64(duration / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func redisNumber(value any) float64 {
	if value == nil {
		return 0
	}
	number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
	if err != nil {
		return 0
	}
	return number
}

func blendMinuteBuckets(current float64, previous float64, now time.Time) float64 {
	previousWeight := float64(60-now.Second()) / 60
	return current + previousWeight*previous
}
