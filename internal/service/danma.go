package service

import (
	"context"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
)

func GetDanmaHistory(ctx context.Context, roomID uint64, cursor uint64, limit int) (*model.MessageCursorDTO, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	danmas, err := repo.SelectMessageByCursor(ctx, roomID, cursor, limit)
	if err != nil {
		log.Printf("get danmas history failed: %v", err)
		return nil, err
	}
	results := &model.MessageCursorDTO{
		List: danmas,
	}
	if len(danmas) == limit {
		results.NextCursor = danmas[len(danmas)-1].ID
	} else {
		results.NextCursor = 0
	}
	return results, nil
}
