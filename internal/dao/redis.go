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

var allowRoomLikeScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], 2)
end
return count
`)

// AllowRoomLike applies a per-user/per-room fixed-window rate limit. It is an
// abuse guard only; durable message_id idempotency is enforced in MySQL.
func AllowRoomLike(ctx context.Context, roomID, userID uint64, maxPerSecond int) (bool, error) {
	if maxPerSecond <= 0 {
		return true, nil
	}
	bucket := time.Now().Unix()
	key := "room:like:rate:" + strconv.FormatUint(roomID, 10) + ":" +
		strconv.FormatUint(userID, 10) + ":" + strconv.FormatInt(bucket, 10)
	count, err := allowRoomLikeScript.Run(ctx, SC.Redis, []string{key}).Int64()
	if err != nil {
		return false, err
	}
	return count <= int64(maxPerSecond), nil
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

// ReconcileRoomLikeCounts copies the durable MySQL count into Redis. During
// the first startup after upgrading from the old Redis-only implementation,
// preserveRedis=true first keeps a larger existing Redis value so deployment
// does not silently erase legacy likes. Periodic reconciliation uses false.
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
		// Never overwrite a larger value observed after the MySQL snapshot was
		// read; a concurrent like may already have advanced both stores.
		if err := restoreRoomLikeCountScript.Run(ctx, SC.Redis, []string{key}, count).Err(); err != nil {
			return 0, fmt.Errorf("restore room %d likes to Redis: %w", rooms[i].RoomID, err)
		}
	}
	return len(rooms), nil
}
