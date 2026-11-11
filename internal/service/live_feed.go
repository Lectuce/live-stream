package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"time"
)

const (
	hotLiveFeedCursorVersion = 1
	hotLiveFeedCandidates    = 500
	hotLiveFeedSnapshotTTL   = 5 * time.Minute
)

var (
	ErrInvalidFeedCursor   = errors.New("invalid feed cursor")
	ErrFeedSnapshotExpired = errors.New("feed snapshot expired")
)

type hotLiveFeedCursor struct {
	Version    int    `json:"version"`
	SnapshotID string `json:"snapshot_id"`
	Offset     int    `json:"offset"`
}

func GetHotLiveFeed(ctx context.Context, cursorText string, limit int) (*model.LiveFeedPage, error) {

	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("invalid limit")
	}

	var snapshotID string

	roomIDs := make([]uint64, 0)
	var offset int

	if cursorText == "" {
		rooms, err := repo.SelectRankedRooms(ctx, hotLiveFeedCandidates)
		if err != nil {
			return nil, fmt.Errorf("select rooms error")
		}
		roomIDs := make([]uint64, 0, len(roomIDs))
		for _, room := range rooms {
			roomIDs = append(roomIDs, room.RoomID)
		}

		snapshotID, err = dao.CreateLiveFeedSnapshot(ctx, roomIDs, hotLiveFeedSnapshotTTL)
		if err != nil {
			return nil, fmt.Errorf("create snapshot error")
		}

	} else {
		cursor, err := decodeHotLiveFeedCursor(cursorText)
		if err != nil {
			return nil, err
		}

		snapshotID = cursor.SnapshotID
		offset = cursor.Offset

		roomIDs, err = dao.ReadLiveFeedSnapshot(ctx, snapshotID)
		if errors.Is(err, dao.ErrLiveFeedSnapshotNotFound) {
			return nil, ErrFeedSnapshotExpired
		}
		if err != nil {
			return nil, err
		}

		if offset > len(roomIDs) {
			return nil, ErrInvalidFeedCursor
		}
	}

	// 从快照中取出还在直播的房间
	items := make([]model.HotRoomDTO, 0, limit)

	for len(items) < limit && offset < len(roomIDs) {
		remaining := limit - len(items)
		end := min(offset+remaining, len(roomIDs))

		candidateIDs := roomIDs[offset:end]

		// 将变量 end 赋值给变量 offset。
		// 表示候选 ID 已经被扫描，即使其中部分直播间已经下播也不回退。
		offset = end

		rooms, err := repo.SelectActiveRoomDTOsByIDs(ctx, candidateIDs)
		if err != nil {
			return nil, err
		}

		items = append(
			items,
			orderLiveFeedRooms(candidateIDs, rooms)...,
		)
	}

	hasMore := offset < len(roomIDs)
	nextCursor := ""

	if hasMore {
		encoded, err := encodeHotLiveFeedCursor(hotLiveFeedCursor{
			Version:    hotLiveFeedCursorVersion,
			SnapshotID: snapshotID,
			Offset:     offset,
		})
		if err != nil {
			return nil, err
		}
		nextCursor = encoded
	}

	return &model.LiveFeedPage{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func orderLiveFeedRooms(
	roomIDs []uint64,
	rooms []model.HotRoomDTO,
) []model.HotRoomDTO {
	byID := make(map[uint64]model.HotRoomDTO, len(rooms))
	for _, room := range rooms {
		byID[room.RoomID] = room
	}

	ordered := make([]model.HotRoomDTO, 0, len(rooms))
	for _, roomID := range roomIDs {
		if room, exists := byID[roomID]; exists {
			ordered = append(ordered, room)
		}
	}

	return ordered
}

func encodeHotLiveFeedCursor(
	cursor hotLiveFeedCursor,
) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal feed cursor: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeHotLiveFeedCursor(value string) (hotLiveFeedCursor, error) {
	var cursor hotLiveFeedCursor

	err := json.Unmarshal([]byte(value), &cursor)
	if err != nil {
		return cursor, ErrInvalidFeedCursor
	}

	if cursor.Version == hotLiveFeedCursorVersion || cursor.SnapshotID == "" || cursor.Offset < 0 {
		return cursor, ErrInvalidFeedCursor
	}

	return cursor, nil

}
