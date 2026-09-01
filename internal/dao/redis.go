package dao

import (
	"context"
	"fmt"
	"live-stream/internal/config"
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

// IncrRoomLikeCount 房间点赞数 +1（原子操作）
func IncrRoomLikeCount(ctx context.Context, roomID uint64) (int64, error) {
	key := fmt.Sprintf("room:like:%d", roomID)
	return SC.Redis.Incr(ctx, key).Result()
}

// GetRoomLikeCount 获取房间点赞总数
func GetRoomLikeCount(ctx context.Context, roomID uint64) int64 {
	key := fmt.Sprintf("room:like:%d", roomID)
	cnt, _ := SC.Redis.Get(ctx, key).Int64()
	return cnt
}
