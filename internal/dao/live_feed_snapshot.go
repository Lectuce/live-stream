package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type LiveFeedSnapshot struct {
	RoomIDs []uint64 `json:"room_ids"`
}

const liveFeedSnapshotPrefix = "feed:live:hot:"

var ErrLiveFeedSnapshotNotFound = errors.New("live feed snapshot not found")

func CreateLiveFeedSnapshot(ctx context.Context, roomIDs []uint64, ttl time.Duration) (string, error) {

	snapshotID := uuid.NewString()

	payload, err := json.Marshal(roomIDs)
	if err != nil {
		return "", fmt.Errorf("marshal live feed snapshot: %w", err)
	}

	key := liveFeedSnapshotPrefix + snapshotID
	err = SC.Redis.Set(ctx, key, payload, ttl).Err()
	if err != nil {
		return "", fmt.Errorf("save live feed snapshot: %w", err)
	}

	return snapshotID, nil

}

func ReadLiveFeedSnapshot(ctx context.Context, snapshotID string) ([]uint64, error) {
	_, err := uuid.Parse(snapshotID)
	if err != nil {
		return nil, ErrLiveFeedSnapshotNotFound
	}

	payload, err := SC.Redis.Get(
		ctx,
		liveFeedSnapshotPrefix+snapshotID,
	).Bytes()

	if errors.Is(err, redis.Nil) {
		return nil, ErrLiveFeedSnapshotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read live feed snapshot: %w", err)
	}

	var roomIDs []uint64
	err = json.Unmarshal(payload, &roomIDs)
	if err != nil {
		return nil, fmt.Errorf("unmarshal live feed snapshot: %w", err)
	}

	return roomIDs, nil

}
