package dao

import (
	"context"
	"fmt"
	"live-stream/internal/config"
	"live-stream/internal/model"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const DanmuChannel = "live:danmu:broadcast"

func InitRedis() {
	cfg := config.GlobalConfig
	Rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		Username: cfg.Redis.Username,
	})
	err := Rdb.Ping(context.Background()).Err()
	if err != nil {
		panic("connect redis failed: " + err.Error())
	}
	SC.Redis = Rdb

}

// 发布
func Publish(ctx context.Context, channel string, msg string) error {
	return SC.Redis.Publish(ctx, channel, msg).Err()
}

// 订阅
func Subscribe(ctx context.Context, channel string) (*redis.PubSub, error) {
	pubSub := SC.Redis.Subscribe(ctx, channel)
	_, err := pubSub.Receive(ctx)
	if err != nil {
		_ = pubSub.Close()
		return nil, err
	}
	return pubSub, nil

}

func SetSession(ctx context.Context, key string, userID uint64, ttl time.Duration) error {
	return SC.Redis.Set(ctx, key, userID, ttl).Err()
}

// GetSession 查询session，返回userID，不存在返回0
func GetSession(ctx context.Context, key string) (uint64, error) {
	val, err := SC.Redis.Get(ctx, key).Uint64()
	if err == redis.Nil {
		// key不存在，会话失效
		return 0, nil
	}
	return val, err
}

// DelSession 删除session（登出时用）
func DelSession(ctx context.Context, key string) error {
	return SC.Redis.Del(ctx, key).Err()
}

// GetRoomLikeCount 获取房间点赞总数
func GetRoomLikeCount(ctx context.Context, roomID uint64) int64 {
	key := fmt.Sprintf("room:like:%d", roomID)
	cnt, _ := SC.Redis.Get(ctx, key).Int64()
	return cnt
}

func SetRoomLikeCount(ctx context.Context, roomID uint64, count int64) error {
	key := fmt.Sprintf("room:like:%d", roomID)
	return SC.Redis.Set(ctx, key, count, 0).Err()
}

// SetRoomLikeCounts 使用流水线写入绝对计数值。网络错误后重试整个流水线是安全的，
// 因为这里执行的是 SET 而不是 INCR 操作。
func SetRoomLikeCounts(ctx context.Context, counts map[uint64]int64) error {
	if len(counts) == 0 {
		return nil
	}
	_, err := SC.Redis.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for roomID, count := range counts {
			pipe.Set(ctx, fmt.Sprintf("room:like:%d", roomID), count, 0)
		}
		return nil
	})
	return err
}

type LikeReservationStatus int64

const (
	LikeReservationRateLimited LikeReservationStatus = iota
	LikeReservationAcquired
	LikeReservationAccepted
	LikeReservationPending
	LikeReservationConflict
)

type RoomLikeIdentity struct {
	MessageID string
	RoomID    uint64
	UserID    uint64
}

var reserveRoomLikeScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current then
  if current == ARGV[1] then
    return 2
  end
  if current == ARGV[2] then
    return 3
  end
  return 4
end
local count = redis.call('INCR', KEYS[2])
if count == 1 then
  redis.call('EXPIRE', KEYS[2], 2)
end
if count > tonumber(ARGV[3]) then
  return 0
end
redis.call('SET', KEYS[1], ARGV[2], 'EX', tonumber(ARGV[4]))
return 1
`)

const confirmRoomLikeLua = `
local current = redis.call('GET', KEYS[1])
if not current or current == ARGV[1] or current == ARGV[2] then
  redis.call('SET', KEYS[1], ARGV[2], 'EX', tonumber(ARGV[3]))
  return 1
end
return 0
`

var confirmRoomLikeScript = redis.NewScript(confirmRoomLikeLua)

var releaseRoomLikeScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// ReserveRoomLike 以原子方式组合短窗口防滥用检查和 message_id 预留。
// 客户端使用相同 UUID 重试时不会创建第二条 Kafka 记录，而 Kafka 写入失败时
// 可以释放该预留。
func ReserveRoomLike(
	ctx context.Context,
	identity RoomLikeIdentity,
	maxPerSecond int,
	pendingTTL time.Duration,
) (LikeReservationStatus, error) {
	if maxPerSecond <= 0 {
		maxPerSecond = 1<<31 - 1
	}
	if pendingTTL <= 0 {
		pendingTTL = time.Minute
	}
	idempotencyKey := roomLikeIdempotencyKey(identity.MessageID)
	rateKey := "room:like:rate:" + strconv.FormatUint(identity.RoomID, 10) + ":" +
		strconv.FormatUint(identity.UserID, 10) + ":" + strconv.FormatInt(time.Now().Unix(), 10)
	accepted, pending := roomLikeReservationValues(identity)
	status, err := reserveRoomLikeScript.Run(
		ctx,
		SC.Redis,
		[]string{idempotencyKey, rateKey},
		accepted,
		pending,
		maxPerSecond,
		int64(pendingTTL/time.Second),
	).Int64()
	return LikeReservationStatus(status), err
}

func ConfirmRoomLike(
	ctx context.Context,
	identity RoomLikeIdentity,
	acceptedTTL time.Duration,
) error {
	if acceptedTTL <= 0 {
		acceptedTTL = 24 * time.Hour
	}
	accepted, pending := roomLikeReservationValues(identity)
	confirmed, err := confirmRoomLikeScript.Run(
		ctx,
		SC.Redis,
		[]string{roomLikeIdempotencyKey(identity.MessageID)},
		pending,
		accepted,
		int64(acceptedTTL/time.Second),
	).Int64()
	if err != nil {
		return err
	}
	if confirmed != 1 {
		return fmt.Errorf("like message_id reservation belongs to another identity")
	}
	return nil
}

// ConfirmRoomLikes 修复一种罕见情况：Kafka 已接受点赞，但网关在延长 Redis
// 幂等预留期限前退出。
func ConfirmRoomLikes(
	ctx context.Context,
	identities []RoomLikeIdentity,
	acceptedTTL time.Duration,
) (conflicts int, err error) {
	if len(identities) == 0 {
		return 0, nil
	}
	if acceptedTTL <= 0 {
		acceptedTTL = 24 * time.Hour
	}
	results := make([]*redis.Cmd, 0, len(identities))
	_, err = SC.Redis.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, identity := range identities {
			accepted, pending := roomLikeReservationValues(identity)
			results = append(results, pipe.Eval(
				ctx,
				confirmRoomLikeLua,
				[]string{roomLikeIdempotencyKey(identity.MessageID)},
				pending,
				accepted,
				int64(acceptedTTL/time.Second),
			))
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, result := range results {
		confirmed, err := result.Int64()
		if err != nil {
			return 0, err
		}
		if confirmed != 1 {
			conflicts++
		}
	}
	return conflicts, nil
}

func ReleaseRoomLikeReservation(ctx context.Context, identity RoomLikeIdentity) error {
	_, pending := roomLikeReservationValues(identity)
	return releaseRoomLikeScript.Run(
		ctx,
		SC.Redis,
		[]string{roomLikeIdempotencyKey(identity.MessageID)},
		pending,
	).Err()
}

func roomLikeIdempotencyKey(messageID string) string {
	return "room:like:idempotency:" + messageID
}

func roomLikeReservationValues(identity RoomLikeIdentity) (accepted string, pending string) {
	fingerprint := strconv.FormatUint(identity.RoomID, 10) + ":" + strconv.FormatUint(identity.UserID, 10)
	return "accepted:" + fingerprint, "pending:" + fingerprint
}

var restoreRoomLikeCountScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
local durable = tonumber(ARGV[1])
if not current or tonumber(current) < durable then
  redis.call('SET', KEYS[1], durable)
  return durable
end
return tonumber(current)
`)

// ReconcileRoomLikeCounts 将 MySQL 中的持久化计数复制到 Redis。
// 从旧版纯 Redis 实现升级后首次启动时，preserveRedis=true 会先保留 Redis 中
// 较大的现有值，避免部署过程静默清除历史点赞。定期对账时使用 false。
func ReconcileRoomLikeCounts(ctx context.Context, preserveRedis bool) (int, error) {
	var rooms []model.Room
	if err := SC.Db.WithContext(ctx).
		Select("room_id", "like_count").
		Find(&rooms).Error; err != nil {
		return 0, fmt.Errorf("list room like counts: %w", err)
	}
	for i := range rooms {
		count := rooms[i].LikeCount
		if preserveRedis {
			redisCount, err := SC.Redis.Get(ctx, fmt.Sprintf("room:like:%d", rooms[i].RoomID)).Int64()
			if err != nil && err != redis.Nil {
				return 0, fmt.Errorf("read legacy Redis likes for room %d: %w", rooms[i].RoomID, err)
			}
			if err == nil && redisCount > count {
				count = redisCount
				if err := SC.Db.WithContext(ctx).Model(&model.Room{}).
					Where("room_id = ?", rooms[i].RoomID).
					UpdateColumn("like_count", count).Error; err != nil {
					return 0, fmt.Errorf("preserve legacy likes for room %d: %w", rooms[i].RoomID, err)
				}
			}
		}
		key := fmt.Sprintf("room:like:%d", rooms[i].RoomID)
		// 读取 MySQL 快照后，如果观察到更大的值，绝不能覆盖；
		// 并发点赞可能已经同时推进了两个存储中的计数。
		if err := restoreRoomLikeCountScript.Run(ctx, SC.Redis, []string{key}, count).Err(); err != nil {
			return 0, fmt.Errorf("restore room %d likes to Redis: %w", rooms[i].RoomID, err)
		}
	}
	return len(rooms), nil
}
