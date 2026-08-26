package repo

import (
	"context"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"

	"gorm.io/gorm"
)

func CreateRoom(ctx context.Context, room *model.Room) (*model.Room, error) {
	// err := gorm.G[model.User](dao.SC.Db).Create(ctx, user)
	// if err != nil {
	// 	panic(fmt.Errorf("fatal error create user: %w", err))
	// }
	results := gorm.WithResult()
	err := gorm.G[model.Room](dao.SC.Db, results).Create(ctx, room)
	if err != nil {
		panic(fmt.Errorf("fatal error create user: %w", err))
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

func UpdateRoom(ctx context.Context, room *model.Room) error {
	rowsaffected, err := gorm.G[model.Room](dao.SC.Db).Where("room_id = ?", room.RoomID).Update(ctx, "room_title", room.RoomTitle)
	if err != nil {
		return fmt.Errorf("fatal error update room: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no record found, RoomID: %v, name: %v", room.RoomID, room.RoomTitle)
	}
}

func UpdateRoomStatus(ctx context.Context, room *model.Room) error {
	rowsaffected, err := gorm.G[model.Room](dao.SC.Db).Where("room_id = ?", room.RoomID).Update(ctx, "room_status", room.Status)
	if err != nil {
		return fmt.Errorf("fatal error update room status: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no record found, RoomID: %v, name: %v, status: %v", room.RoomID, room.RoomTitle, room.Status)
	}
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
		return nil, fmt.Errorf("fatal error select user: %w", err)
	}

	return &resroom, nil
}
