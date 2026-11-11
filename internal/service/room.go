package service

import (
	"context"
	"encoding/json"
	"errors"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
)

var ErrInvalidOwnerID = errors.New("user already has a room")
var ErrInvalidRoomID = errors.New("room is not exist")
var ErrNotRoomOwner = errors.New("only room owner can operate")

func CreateRoom(c context.Context, ownerID uint64, req *model.CreateRoomRequest) (*model.Room, error) {
	if req == nil {
		return nil, errors.New("create room request is nil")
	}
	var room model.Room
	room = model.Room{
		OwnerID:   ownerID,
		RoomTitle: req.Title,
		Status:    0,
	}
	// 查询用户ID是否拥有房间
	resroom, err := repo.SelectRoomByOwnerID(c, ownerID)
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
	limit := config.GlobalConfig.StreamList.Count
	if limit <= 0 {
		limit = 20
	}
	rooms, err := repo.SelectRankedRooms(c, limit)
	if err != nil {
		log.Printf("list rooms failed: %v", err)
		return nil, err
	}
	return rooms, nil
}

func GetRoomByOwnerID(ctx context.Context, ownerID uint64) (*model.Room, error) {
	return repo.SelectRoomByOwnerID(ctx, ownerID)
}

func RoomDetail(c context.Context, roomID uint64) (*model.RoomDetail, error) {
	detail, err := repo.SelectRoomDetail(c, roomID)
	if err != nil {
		log.Printf("room detail failed: %v", err)
		return nil, err
	}
	return &detail, nil
}

func UpdateRoom(c context.Context, updateReq *model.UpdateRoomRequest, loginID uint64, roomID uint64) (*model.Room, error) {
	if updateReq == nil {
		return nil, errors.New("update room request is nil")
	}
	room, err := repo.SelectRoomByRoomID(c, roomID)
	if err != nil {
		log.Printf("room update failed: %v", err)
		return nil, err
	}
	if room == nil {
		return nil, ErrInvalidRoomID
	}
	// 房主权限校验
	if room.OwnerID != loginID {
		return nil, ErrNotRoomOwner
	}
	roomTitle := updateReq.RoomTitle
	err = repo.UpdateRoom(c, roomID, roomTitle)
	if err != nil {
		log.Printf("room update failed: %v", err)
		return nil, err
	}
	room, err = repo.SelectRoomByRoomID(c, roomID)
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

func StopLiveService(ctx context.Context, roomID uint64, loginID uint64) error {
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
	if room.Status != 1 {
		return errors.New("room is not live")
	}
	err = repo.StopLive(ctx, roomID)
	if err != nil {
		return err
	}

	message := model.Message{
		Type:   model.MsgTypeSystem,
		RoomID: roomID,
		Data: map[string]interface{}{
			"sub_type": "room_status",
			"status":   0,
			"reason":   "stream_ended",
		},
	}

	payload, err := json.Marshal(message)
	if err != nil {
		log.Printf("marshal room status event failed, room_id=%d, err=%v", roomID, err)
		return nil
	}

	err = dao.Publish(ctx, dao.DanmuChannel, string(payload))
	if err != nil {
		// 数据库已经成功关播，不能因为广播失败向主播返回关播失败。
		// 观众端后续可以通过状态查询纠正。
		log.Printf("publish room status event failed, room_id=%d, err=%v", roomID, err)
	}

	return nil
}
