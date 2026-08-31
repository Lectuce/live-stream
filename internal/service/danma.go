package service

import (
	"context"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
)

func GetDanmaHistory(ctx context.Context, roomID uint64, cursor uint64, limit int) (*model.MessageCursorDTO, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	danmas, err := repo.SelectMessageByCursor(ctx, roomID, cursor, limit+1)
	if err != nil {
		log.Printf("get danmas history failed: %v", err)
		return nil, err
	}
	hasMore := len(danmas) > limit
	if hasMore {
		danmas = danmas[:limit]
	}

	var nextCursor uint64
	if hasMore && len(danmas) > 0 {
		nextCursor = danmas[len(danmas)-1].ID
	}
	return &model.MessageCursorDTO{
		Items:       danmas,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}
