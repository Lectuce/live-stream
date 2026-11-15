package dao

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type CommentSnapshot struct {
	TargetType string   `json:"target_type"`
	TargetID   uint64   `json:"target_id"`
	IDs        []uint64 `json:"ids"`
	Truncated  bool     `json:"truncated"`
}

var ErrCommentSnapshotExpired = errors.New("comment snapshot expired")

func SaveCommentSnapshot(ctx context.Context, snapshot CommentSnapshot) (string, error) {
	if SC.Redis == nil {
		return "", errors.New("redis unavailable")
	}
	id := uuid.NewString()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	return id, SC.Redis.Set(ctx, "feed:comment:hot:"+id, data, 5*time.Minute).Err()
}

func ReadCommentSnapshot(ctx context.Context, id string) (CommentSnapshot, error) {
	var snapshot CommentSnapshot
	if SC.Redis == nil {
		return snapshot, errors.New("redis unavailable")
	}
	data, err := SC.Redis.Get(ctx, "feed:comment:hot:"+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return snapshot, ErrCommentSnapshotExpired
	}
	if err != nil {
		return snapshot, err
	}
	err = json.Unmarshal(data, &snapshot)
	return snapshot, err
}
