package dao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"time"
)

var ErrVideoSnapshotExpired = errors.New("video snapshot expired")
var ErrVideoDiscoveryUnavailable = errors.New("video discovery unavailable")

type VideoSnapshot struct {
	Mode, Category string
	IDs            []uint64
}

func VideoSnapshotRead(ctx context.Context, id string) (VideoSnapshot, error) {
	var s VideoSnapshot
	if _, err := uuid.Parse(id); err != nil {
		return s, ErrVideoSnapshotExpired
	}
	if SC == nil || SC.Redis == nil {
		return s, ErrVideoDiscoveryUnavailable
	}
	b, err := SC.Redis.Get(ctx, "feed:video:snapshot:"+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return s, ErrVideoSnapshotExpired
	}
	if err != nil {
		return s, ErrVideoDiscoveryUnavailable
	}
	if json.Unmarshal(b, &s) != nil || len(s.IDs) > 500 {
		return s, ErrVideoSnapshotExpired
	}
	return s, nil
}
func VideoSnapshotCurrent(ctx context.Context, mode, category string, build func() ([]uint64, error)) (string, VideoSnapshot, error) {
	var s VideoSnapshot
	if SC == nil || SC.Redis == nil {
		return "", s, ErrVideoDiscoveryUnavailable
	}
	key := "feed:video:current:" + mode + ":" + category
	readCtx, readCancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer readCancel()
	if id, err := SC.Redis.Get(readCtx, key).Result(); err == nil {
		s, e := VideoSnapshotRead(readCtx, id)
		return id, s, e
	} else if !errors.Is(err, redis.Nil) {
		return "", s, ErrVideoDiscoveryUnavailable
	}
	lock := key + ":lock"
	token := uuid.NewString()
	ok, err := SC.Redis.SetNX(readCtx, lock, token, 10*time.Second).Result()
	if err != nil || !ok {
		return "", s, ErrVideoDiscoveryUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		SC.Redis.Eval(cleanup, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{lock}, token)
	}()
	// Double check after taking the rebuild lease.
	if id, err := SC.Redis.Get(readCtx, key).Result(); err == nil {
		s, e := VideoSnapshotRead(readCtx, id)
		return id, s, e
	}
	ids, err := build()
	if err != nil {
		return "", s, err
	}
	s = VideoSnapshot{Mode: mode, Category: category, IDs: ids}
	b, _ := json.Marshal(s)
	id := uuid.NewString()
	writeCtx, writeCancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer writeCancel()
	_, err = SC.Redis.TxPipelined(writeCtx, func(p redis.Pipeliner) error {
		p.Set(writeCtx, "feed:video:snapshot:"+id, b, 10*time.Minute)
		p.Set(writeCtx, key, id, time.Minute)
		return nil
	})
	if err != nil {
		return "", s, ErrVideoDiscoveryUnavailable
	}
	return id, s, nil
}
func VideoDeliveryToken(ctx context.Context, ids []uint64) string {
	if len(ids) == 0 || SC == nil || SC.Redis == nil {
		return ""
	}
	b, _ := json.Marshal(ids)
	sum := sha256.Sum256(b)
	token := hex.EncodeToString(sum[:])
	if SC.Redis.Set(ctx, "feed:video:delivery:"+token, b, 10*time.Minute).Err() != nil {
		return ""
	}
	return token
}
func ValidVideoDelivery(ctx context.Context, token string, video uint64) bool {
	if len(token) != 64 || SC == nil || SC.Redis == nil {
		return false
	}
	if _, err := hex.DecodeString(token); err != nil {
		return false
	}
	b, err := SC.Redis.Get(ctx, "feed:video:delivery:"+token).Bytes()
	if err != nil {
		return false
	}
	var ids []uint64
	if json.Unmarshal(b, &ids) != nil {
		return false
	}
	for _, id := range ids {
		if id == video {
			return true
		}
	}
	return false
}
