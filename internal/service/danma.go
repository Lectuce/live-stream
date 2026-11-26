package service

import (
	"context"
	"errors"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
	"sync"
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
	if err := repo.RedactHiddenDanmas(ctx, danmas); err != nil {
		return nil, err
	}
	return &model.MessageCursorDTO{
		Items:      danmas,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

var replayDatabaseSlots chan struct{}
var replaySlotsOnce sync.Once
var ErrReplayBusy = errors.New("replay database busy")

func ReplayDanmaHistory(ctx context.Context, roomID, afterSeq uint64, limit int) (*model.MessageReplayDTO, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > config.ReplaySettings().MaxPageSize {
		limit = config.ReplaySettings().MaxPageSize
	}

	messages, covered, cacheErr := dao.SelectRecentDanmasAfterSeq(ctx, roomID, afterSeq, limit+1)
	source := "redis"
	// Both fallback and moderation read MySQL. Keep one shared bounded permit.
	replaySlotsOnce.Do(func() { replayDatabaseSlots = make(chan struct{}, config.ReplaySettings().MaxMysqlInflight) })
	select {
	case replayDatabaseSlots <- struct{}{}:
		defer func() { <-replayDatabaseSlots }()
	default:
		return nil, ErrReplayBusy
	}

	if cacheErr != nil {
		log.Printf("read recent danmaku cache failed, fallback to mysql: %v", cacheErr)
	}
	if cacheErr != nil || !covered {
		var err error
		messages, err = repo.SelectMessagesAfterSeq(ctx, roomID, afterSeq, limit+1)
		if err != nil {
			return nil, err
		}
		source = "mysql"
	}
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	nextSeq := afterSeq
	if len(messages) > 0 {
		nextSeq = messages[len(messages)-1].RoomSeq
	}
	if err := repo.RedactHiddenDanmas(ctx, messages); err != nil {
		return nil, err
	}
	return &model.MessageReplayDTO{
		Items:   messages,
		NextSeq: nextSeq,
		HasMore: hasMore,
		Source:  source,
	}, nil
}
