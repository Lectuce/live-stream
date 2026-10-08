package repo

import (
	"context"
	"errors"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"time"

	"gorm.io/gorm"
)

func CreateRoom(ctx context.Context, room *model.Room) (*model.Room, error) {
	// err := gorm.G[model.User](dao.SC.Db).Create(ctx, user)
	// if err != nil {
	// 	panic(fmt.Errorf("创建用户失败: %w", err))
	// }
	results := gorm.WithResult()
	err := gorm.G[model.Room](dao.SC.Db, results).Create(ctx, room)
	if err != nil {
		return nil, fmt.Errorf("fatal error create room: %w", err)
	}
	if results.RowsAffected == 0 {
		return nil, fmt.Errorf("no rows inserted")
	}

	return room, nil
}

func DeleteRoom(ctx context.Context, room *model.Room) error {
	rowsaffected, err := gorm.G[model.Room](dao.SC.Db).Where("room_id = ? ", room.RoomID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("fatal error delete room: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no record found, RoomID: %v", room.RoomID)
	}

}

func UpdateRoom(ctx context.Context, roomID uint64, roomTitle string) error {
	rowsaffected, err := gorm.G[model.Room](dao.SC.Db).Where("room_id = ?", roomID).Update(ctx, "room_title", roomTitle)
	if err != nil {
		return fmt.Errorf("fatal error update room: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no record found, RoomID: %v, name: %v", roomID, roomTitle)
	}
}

func UpdateRoomStatus(ctx context.Context, roomID uint64, targetStatus int) error {
	rowsAffected, err := gorm.G[model.Room](dao.SC.Db).
		Where("room_id = ?", roomID).
		Update(ctx, "status", targetStatus)
	if err != nil {
		return fmt.Errorf("fatal error update room status: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("no record found, roomID: %d", roomID)
	}
	return nil
}

func SelectRooms(ctx context.Context, room *model.Room) ([]model.Room, error) {

	rooms, err := gorm.G[model.Room](dao.SC.Db).Where("room_title LIKE ?", "%"+room.RoomTitle+"%").Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("fatal error select user: %w", err)
	}

	return rooms, nil
}

func SelectRoom(ctx context.Context, room *model.Room) (*model.Room, error) {
	resroom, err := gorm.G[model.Room](dao.SC.Db).Where("room_id = ?", room.RoomID).First(ctx)

	if err != nil {
		return nil, fmt.Errorf("fatal error select room: %w", err)
	}

	return &resroom, nil
}

func SelectRoomByRoomID(ctx context.Context, roomID uint64) (*model.Room, error) {
	resroom, err := gorm.G[model.Room](dao.SC.Db).Where("room_id = ?", roomID).First(ctx)
	if err != nil {
		return nil, fmt.Errorf("fatal error select room by room id: %w", err)
	}
	return &resroom, nil
}

func SelectRoomByOwnerID(ctx context.Context, ownerID uint64) (*model.Room, error) {
	resroom, err := gorm.G[model.Room](dao.SC.Db).Where("owner_id = ?", ownerID).First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 没有房间是正常结果，表示用户可以创建房间
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fatal error select room by owner id: %w", err)
	}
	return &resroom, nil
}

func SelectActiveRoomHeatSources(ctx context.Context) ([]model.ActiveRoomHeatSource, error) {
	rooms, err := gorm.G[model.ActiveRoomHeatSource](dao.SC.Db).
		Raw(`SELECT room_id, start_at FROM rooms WHERE status = ?`, 1).
		Find(ctx)
	if err != nil {
		return nil, err
	}
	return rooms, nil
}

// SelectRankedRooms 使用 Redis 中的临时热度分数排序，并从 MySQL 补全房间记录。
// 排行数据缺失或不可用时降级为按最新开播房间排序，绝不退回按历史
// like_count 排序。
func SelectRankedRooms(ctx context.Context, limit int) ([]model.HotRoomDTO, error) {
	if limit <= 0 {
		return nil, nil
	}
	ranking, rankingErr := dao.ReadHotRoomRanking(ctx, limit*2)
	if rankingErr == nil && len(ranking) > 0 {
		ids := make([]uint64, 0, len(ranking))
		scores := make(map[uint64]float64, len(ranking))
		for _, item := range ranking {
			ids = append(ids, item.RoomID)
			scores[item.RoomID] = item.Score
		}
		rooms, err := selectActiveRoomDTOsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		ordered, seen := orderRoomsByHotRanking(ranking, rooms, scores, limit)
		if len(ordered) == limit {
			return ordered, nil
		}
		fallback, err := selectNewestActiveRooms(ctx, limit)
		if err != nil {
			return nil, err
		}
		for _, room := range fallback {
			if _, exists := seen[room.RoomID]; exists {
				continue
			}
			ordered = append(ordered, room)
			if len(ordered) == limit {
				break
			}
		}
		return ordered, nil
	}
	return selectNewestActiveRooms(ctx, limit)
}

func orderRoomsByHotRanking(
	ranking []dao.HotRoomRank,
	rooms []model.HotRoomDTO,
	scores map[uint64]float64,
	limit int,
) ([]model.HotRoomDTO, map[uint64]struct{}) {
	byID := make(map[uint64]model.HotRoomDTO, len(rooms))
	for _, room := range rooms {
		room.HotScore = scores[room.RoomID]
		byID[room.RoomID] = room
	}
	ordered := make([]model.HotRoomDTO, 0, limit)
	seen := make(map[uint64]struct{}, limit)
	for _, item := range ranking {
		room, exists := byID[item.RoomID]
		if !exists {
			continue
		}
		ordered = append(ordered, room)
		seen[room.RoomID] = struct{}{}
		if len(ordered) == limit {
			break
		}
	}
	return ordered, seen
}

func selectNewestActiveRooms(ctx context.Context, limit int) ([]model.HotRoomDTO, error) {
	sql := `
		SELECT room.room_id, room.room_title, room.owner_id, u.user_name as owner_username, room.like_count
		FROM rooms room
		LEFT JOIN users u ON  u.user_id= room.owner_id
		WHERE room.status = ?
		ORDER BY room.start_at DESC, room.room_id DESC
		LIMIT ?
	`
	list, err := gorm.G[model.HotRoomDTO](dao.SC.Db).Raw(sql, 1, limit).Find(ctx)
	if err != nil {
		return nil, err
	}
	populateRoomPlayURLs(list)
	return list, nil
}

func selectActiveRoomDTOsByIDs(ctx context.Context, roomIDs []uint64) ([]model.HotRoomDTO, error) {
	if len(roomIDs) == 0 {
		return nil, nil
	}
	sql := `
		SELECT room.room_id, room.room_title, room.owner_id, u.user_name as owner_username, room.like_count
		FROM rooms room
		LEFT JOIN users u ON u.user_id = room.owner_id
		WHERE room.status = ? AND room.room_id IN ?
	`
	list, err := gorm.G[model.HotRoomDTO](dao.SC.Db).Raw(sql, 1, roomIDs).Find(ctx)
	if err != nil {
		return nil, err
	}
	populateRoomPlayURLs(list)
	return list, nil
}

func populateRoomPlayURLs(list []model.HotRoomDTO) {
	const playDomain = "https://live.example.com/live"
	for i := range list {
		list[i].PlayURL = fmt.Sprintf("%s/%d.m3u8", playDomain, list[i].RoomID)
	}
}

func SelectRoomDetail(ctx context.Context, roomID uint64) (model.RoomDetail, error) {
	sql := `SELECT room.room_id, room.room_title, room.owner_id, room.start_at, room.status, u.user_name as owner_username, room.like_count
			FROM rooms room
			LEFT JOIN users u ON  u.user_id= room.owner_id
			WHERE room.room_id = ?
	`
	detail, err := gorm.G[model.RoomDetail](dao.SC.Db).Raw(sql, roomID).First(ctx)
	if err != nil {
		return model.RoomDetail{}, err
	}
	const playDomain = "https://live.example.com/live"
	detail.PlayURL = fmt.Sprintf("%s/%d.m3u8", playDomain, roomID)
	return detail, nil
}

func StartLive(ctx context.Context, roomID uint64, title string) error {
	now := time.Now()
	room := model.Room{
		Status:    1,
		RoomTitle: title,
		StartAt:   &now,
	}
	rowsAffected, err := gorm.G[model.Room](dao.SC.Db).Where("room_id = ?", roomID).Updates(ctx, room)
	if err != nil {
		return fmt.Errorf("start live update failed: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("room not found")
	}
	return nil
}

func StopLive(ctx context.Context, roomID uint64) error {
	return UpdateRoomStatus(ctx, roomID, 0)
}
