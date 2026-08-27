package repo

import (
	"context"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"

	"gorm.io/gorm"
)

func CreateDanma(ctx context.Context, danma *model.Danma) (*model.Danma, error) {
	// err := gorm.G[model.User](model.DB.DB).Create(ctx, user)
	// if err != nil {
	// 	panic(fmt.Errorf("fatal error create user: %w", err))
	// }
	results := gorm.WithResult()
	err := gorm.G[model.Danma](dao.SC.Db, results).Create(ctx, danma)
	if err != nil {
		return nil, fmt.Errorf("fatal error create user: %w", err)
	}
	if results.RowsAffected == 0 {
		return nil, fmt.Errorf("no rows inserted")
	}

	return danma, nil
}

func SelectUserDanmas(ctx context.Context, danma *model.Danma) ([]model.Danma, error) {

	danmas, err := gorm.G[model.Danma](dao.SC.Db).Where("user_id = ?", danma.UserID).Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("fatal error select user damas: %w", err)
	}

	return danmas, nil
}

func SelectRoomDanmas(ctx context.Context, danma *model.Danma) ([]model.Danma, error) {

	danmas, err := gorm.G[model.Danma](dao.SC.Db).Where("room_id = ?", danma.RoomID).Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("fatal error select room danmas: %w", err)
	}

	return danmas, nil
}

func DeleteUserDanmasFromRoom(ctx context.Context, danma *model.Danma) error {
	rowsaffected, err := gorm.G[model.Danma](dao.SC.Db).Where("user_id = ? AND room_id = ?", danma.UserID, danma.RoomID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("fatal error delete user danmas from room: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no danma record found, user_id: %v, room_id: %v", danma.UserID, danma.RoomID)
	}

}

func DeleteUserDanma(ctx context.Context, danma *model.Danma) error {
	rowsaffected, err := gorm.G[model.Danma](dao.SC.Db).Where("user_id = ? ", danma.UserID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("fatal error delete user danmas: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no danma record found, user_id: %v", danma.UserID)
	}
}

func DeleteRoomDanma(ctx context.Context, danma *model.Danma) error {
	rowsaffected, err := gorm.G[model.Danma](dao.SC.Db).Where("room_id = ? ", danma.RoomID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("fatal error delete user danmas: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no danma record found in room_id: %v", danma.RoomID)
	}
}
