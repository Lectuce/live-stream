package dao

import (
	"context"
	"live-stream/internal/config"

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
