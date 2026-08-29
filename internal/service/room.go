package service

import (
	"context"
	"errors"
	"live-stream/internal/config"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
)

var ErrInvalidOwnerID = errors.New("user already has a room")
var ErrInvalidRoomID = errors.New("room is not exist")
var ErrNotRoomOwner = errors.New("only room owner can operate")

func CreateRoom(c context.Context, req *model.CreateRoomRequest) (*model.Room, error) {
	if req == nil {
		return nil, errors.New("create room request is nil")
	}
	var room model.Room
	room = model.Room{
		OwnerID: req.OwnerID,
		Status:  0,
		PushURL: req.PushURL,
	}
	// 查询用户ID是否拥有房间
	resroom, err := repo.SelectRoomByOwnerID(c, req.OwnerID)
	if err != nil {
		log.Printf("create room failed: %v", err)
		return nil, err
	}
	// 如果该用户ID已经创建过房间
	if resroom != nil {
		return nil, ErrInvalidOwnerID
	}
	newRoom, err := repo.CreateRoom(c, &room)
	if err != nil {
		log.Printf("create room failed: %v", err)
		return nil, err
	}
	return newRoom, nil
}

func ListRooms(c context.Context) ([]model.HotRoomDTO, error) {
	var rooms []model.HotRoomDTO
	rooms, err := repo.SelectAllRoomsByLikes(c, config.GlobalConfig.StreamList.Count)
	if err != nil {
		log.Printf("list rooms failed: %v", err)
		return nil, err
	}
	return rooms, nil
}

func RoomDetail(c context.Context, roomID uint64) (*model.RoomDetail, error) {
	detail, err := repo.SelectRoomDetail(c, roomID)
	if err != nil {
		log.Printf("room detail failed: %v", err)
		return nil, err
	}
	return &detail, nil
}

func UpdateRoom(c context.Context, updateReq *model.UpdateRoomRequest) (*model.Room, error) {
	if updateReq == nil {
		return nil, errors.New("update room request is nil")
	}
	room, err := repo.SelectRoomByRoomID(c, updateReq.RoomID)
	if err != nil {
		log.Printf("room update failed: %v", err)
		return nil, err
	}
	if room == nil {
		return nil, ErrInvalidRoomID
	}

	err = repo.UpdateRoom(c, updateReq)
	if err != nil {
		log.Printf("room update failed: %v", err)
		return nil, err
	}
	room, err = repo.SelectRoomByRoomID(c, updateReq.RoomID)
	if err != nil {
		log.Printf("room update failed: %v", err)
		return nil, err
	}

	return room, nil

}

func StartLiveService(ctx context.Context, roomID uint64, title string, loginID uint64) error {
	// 当前房间必须是未开播0，不能重复开播
	room, err := repo.SelectRoomByRoomID(ctx, roomID)
	if err != nil {
		return err
	}
	if room == nil {
		return ErrInvalidRoomID
	}
	// 房主权限校验
	if room.OwnerID != loginID {
		return ErrNotRoomOwner
	}
	if room.Status != 0 {
		return errors.New("room is already live")
	}
	return repo.StartLive(ctx, roomID, title)
}

func StopLiveService(ctx context.Context, roomID uint64) error {
	err := repo.StopLive(ctx, roomID)
	if err != nil {
		return err
	}

	return nil
}
